# NATS JetStream Broker for go-micro v5

為 go-micro v5 設計的生產就緒 NATS JetStream broker 實作，提供可靠的訊息發布和基於拉取的消費機制，內建容錯能力和優雅關閉。

[English Documentation](README.md)

## 概述

此 broker 將 NATS JetStream 的持久化訊息功能與 go-micro 的 broker 介面整合，實現可靠的事件驅動微服務通訊。採用批次處理的拉取式消費者，提供最佳吞吐量和背壓管理。

## 主要特性

### 核心功能
- **拉取式訂閱**：背景工作程式以可配置的批次大小擷取訊息
- **同步發布**：等待 JetStream 確認以保證持久化
- **自動建立串流**：首次發布時自動建立串流，使用合理的預設值
- **持久化消費者**：佇列名稱映射到 JetStream 持久化消費者以實現負載平衡
- **訊息順序**：批次內的順序處理維持每個消費者的訊息順序

### 生產標準
- **併發安全**：`sync.RWMutex` 保護跨 goroutine 的 broker 狀態
- **panic 恢復**：兩層恢復機制（擷取迴圈 + 處理器）防止工作程式崩潰
- **指數退避**：擷取錯誤觸發 1 秒 → 30 秒退避，防止緊密迴圈
- **優雅關閉**：`nc.Drain()` 在斷開連線前等待處理中的訊息
- **明確確認**：處理器根據處理結果控制 Ack/Nak

## 安裝

```bash
go get go-micro-jetstream
```

### 依賴項
- `go-micro.dev/v5` - Broker 介面
- `github.com/nats-io/nats.go` - NATS JetStream 客戶端
- `github.com/google/uuid` - 訂閱 ID 生成

## 快速開始

### 1. 啟動 NATS 並啟用 JetStream

```bash
# 使用 Docker
docker run -d --name nats-jetstream \
  -p 4222:4222 -p 8222:8222 \
  nats:latest -js -m 8222

# 或使用原生二進制檔
nats-server -js -m 8222
```

### 2. 建立 Broker

```go
package main

import (
    "time"
    "go-micro.dev/v5/broker"
    jsbroker "go-micro-jetstream/pkg/broker"
)

func main() {
    b := jsbroker.NewBroker(
        broker.Addrs("localhost:4222"),
        jsbroker.WithBatchSize(10),
        jsbroker.WithFetchWait(5*time.Second),
    )

    if err := b.Connect(); err != nil {
        panic(err)
    }
    defer b.Disconnect()
}
```

### 3. 訂閱訊息

```go
handler := func(e broker.Event) error {
    msg := e.Message()
    fmt.Printf("收到訊息: %s\n", string(msg.Body))
    return nil  // 成功時 Ack，返回錯誤則 Nak
}

sub, err := b.Subscribe(
    "orders.created",
    handler,
    broker.Queue("order-processor"),  // 必需：持久化消費者名稱
)
if err != nil {
    panic(err)
}
defer sub.Unsubscribe()
```

### 4. 發布訊息

```go
msg := &broker.Message{
    Header: map[string]string{"type": "order"},
    Body:   []byte(`{"id": "123", "amount": 99.99}`),
}

if err := b.Publish("orders.created", msg); err != nil {
    panic(err)
}
```

## 配置選項

### Broker 選項

```go
// NATS 伺服器位址
broker.Addrs("nats://localhost:4222")

// Fetch 操作的批次大小（預設：10）
jsbroker.WithBatchSize(20)

// Fetch 超時時間（預設：5 秒）
jsbroker.WithFetchWait(3 * time.Second)

// NATS 客戶端名稱（預設：go-micro-{hostname}）
jsbroker.WithClientName("my-service")

// 自訂 NATS 選項，用於 TLS/認證
jsbroker.WithNATSOptions(
    nats.UserInfo("user", "password"),
    nats.RootCAs("./certs/ca.pem"),
)

// 自訂串流配置模板
jsbroker.WithStreamConfig(jetstream.StreamConfig{
    Retention: jetstream.LimitsPolicy,
    MaxAge:    24 * time.Hour,
})
```

### 訂閱選項

```go
// 持久化消費者名稱（必需）
// 直接映射到 JetStream 持久化消費者名稱
broker.Queue("my-consumer-group")

// 自訂 context 用於取消
broker.SubscribeContext(ctx)
```

### 發布選項

```go
// 帶超時的自訂 context
broker.PublishContext(ctx)
```

## 架構

### 串流命名慣例

主題使用第一個片段映射到串流：

```
orders.created     → ORDERS 串流
orders.updated     → ORDERS 串流
user-events.login  → USER_EVENTS 串流
```

串流自動建立時使用：
- **保留策略**：WorkQueue（消費後刪除訊息）
- **儲存**：File（持久化）
- **主題**：`{前綴}.>` 萬用字元模式

### 拉取消費者工作程式模式

每個訂閱產生一個專用的 goroutine：

```
┌─────────────────────────────────────┐
│  訂閱工作程式 Goroutine              │
│                                     │
│  迴圈：                             │
│    1. 從 JetStream Fetch(batchSize) │
│    2. 對每個訊息：                  │
│       - 包裝為 broker.Event         │
│       - 呼叫處理器（帶 recover）    │
│       - 成功時 Ack，錯誤時 Nak      │
│    3. 錯誤時指數退避                │
│    4. 檢查 context 取消             │
└─────────────────────────────────────┘
```

優點：
- 透過批次擷取實現自然背壓
- 透過 context 取消實現乾淨關閉
- 每個訂閱獨立的錯誤處理

### 持久化消費者

佇列名稱是**必需的**，並直接映射到 JetStream 持久化消費者名稱：

```go
// 多個實例使用相同的佇列共享訊息傳遞
broker.Queue("order-processor")  // 建立持久化消費者 "order-processor"
```

**注意**：所有訂閱都必須透過 `broker.Queue()` 提供佇列名稱。這確保：
- 明確的消費者命名以提高可觀察性
- 跨服務實例的負載平衡
- 重啟後消費者狀態持久化
- 從最後確認位置重播訊息

### 持久化消費者命名規則

佇列名稱必須遵循 NATS JetStream 命名限制：

**允許的字元**：
- 字母數字：`a-z`、`A-Z`、`0-9`
- 連字號：`-`
- 底線：`_`

**禁止的字元**：
- 空白字元（空格、Tab、換行）
- 句點：`.`
- 星號：`*`
- 大於號：`>`
- 路徑分隔符號：`/` 或 `\`
- 不可列印字元

**建議**：
- 保持名稱在 32 字元以內以確保檔案系統相容性
- 使用描述性名稱：`order-processor-v2` 而非 `q1`

**範例**：
```go
// 有效的名稱
broker.Queue("order-processor")
broker.Queue("user_events_handler")
broker.Queue("payment-service-v2")

// 無效的名稱（將返回錯誤）
broker.Queue("my.queue")      // 包含句點
broker.Queue("my queue")      // 包含空格
broker.Queue("orders/handler") // 包含斜線
```

## 錯誤處理

### 處理器錯誤

返回錯誤以觸發訊息重新傳遞：

```go
handler := func(e broker.Event) error {
    if err := processMessage(e.Message()); err != nil {
        // 訊息將被 Nak 並重新傳遞
        return err
    }
    return nil  // 訊息將被 Ack
}
```

### 處理器 Panic

Panic 會被恢復並記錄。訊息會被 Nak 以重新傳遞：

```go
handler := func(e broker.Event) error {
    panic("糟糕")  // 已恢復，已記錄，訊息被 Nak
}
```

### 擷取錯誤

網路或 JetStream 錯誤觸發指數退避：

```
錯誤 → 等待 1 秒 → 重試
錯誤 → 等待 2 秒 → 重試
錯誤 → 等待 4 秒 → 重試
...
錯誤 → 等待 30 秒 → 重試（最大值）
```

成功後退避重設為 1 秒。

## 測試

### 執行驗證腳本

包含的驗證腳本展示完整功能：

```bash
# 確保 NATS 與 JetStream 正在執行
docker run -d -p 4222:4222 -p 8222:8222 nats:latest -js -m 8222

# 建置並執行
go build -o jetstream-broker
./jetstream-broker
```

預期輸出：
```
Connected to NATS at nats://localhost:4222
Created stream TEST for topic test.messages
Subscribed to topic test.messages with durable consumer validation-queue
Publishing test messages...
Published message 1
Received message: Test message 1
Published message 2
Received message: Test message 2
...
```

### 驗證 JetStream 狀態

```bash
# 檢查串流
curl http://localhost:8222/jsz?streams=true

# 檢查消費者
curl http://localhost:8222/jsz?consumers=true
```

## 生產環境考量

### 冪等性

JetStream 保證至少一次傳遞。設計處理器時要實現冪等性：

```go
handler := func(e broker.Event) error {
    // 檢查是否已處理
    if alreadyProcessed(e.Message().Header["message-id"]) {
        return nil  // Ack 但不重新處理
    }

    // 處理並標記為完成
    return process(e.Message())
}
```

### 訊息順序

保證每個消費者的順序，而非全域順序：
- 發送到同一消費者的訊息按順序到達
- 多個消費者可能以不同順序處理訊息
- 使用訊息 ID 或時間戳進行跨消費者排序

### 消費者狀態清理

持久化消費者在 `Unsubscribe()` 後仍然存在。要刪除：

```bash
# 使用 NATS CLI
nats consumer delete STREAM_NAME CONSUMER_NAME

# 或透過 HTTP API
curl -X DELETE http://localhost:8222/jsapi/v1/streams/TEST/consumers/validation-queue
```

### 監控

需要追蹤的關鍵指標：
- 發布的訊息數（計數器）
- 消費的訊息數（計數器）
- 處理器錯誤數（計數器）
- 處理器 panic 數（計數器）
- 擷取錯誤數（計數器）
- 活動訂閱數（量表）

檢查 JetStream 監控：
```bash
curl http://localhost:8222/jsz
```

### 效能調校

**高吞吐量**：增加批次大小
```go
jsbroker.WithBatchSize(100)
```

**低延遲**：減少擷取等待時間
```go
jsbroker.WithFetchWait(1 * time.Second)
```

**大型訊息**：使用基於位元組的批次處理（未來增強功能）

## 設計原則

此實作遵循 Linus Torvalds 的軟體哲學：

1. **簡單勝過聰明**：直接使用 NATS 類型，沒有不必要的抽象
2. **資料結構優先**：圍繞清晰的 broker 結構建立狀態機
3. **明確的錯誤處理**：沒有靜默失敗，所有錯誤都被記錄和傳播
4. **效能很重要**：批次擷取，最小化記憶體分配，高效鎖定
5. **良好的程式碼品味**：拉取工作程式顯而易見，而非聰明

## 限制

- 僅支援拉取消費者（不支援推送消費者）
- 僅支援同步發布（沒有非同步批次處理）
- 自動建立的串流使用固定的命名慣例
- 沒有內建的指標/追蹤（透過中介軟體添加）

## 授權

MIT

## 貢獻

歡迎提交 Pull Request。請確保：
- 程式碼或日誌中不使用表情符號
- 遵循 Go 最佳實踐
- 生產就緒的錯誤處理
- 測試通過
- 文件已更新

## 相關連結

- [NATS JetStream 文件](https://docs.nats.io/nats-concepts/jetstream)
- [go-micro 文件](https://go-micro.dev)
- [範例服務](./examples/)（如果可用）
