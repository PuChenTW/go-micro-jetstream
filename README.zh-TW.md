# NATS JetStream Driver for Go

一個專為現代 Go 應用程式設計的強大、具備 Context 感知的 NATS JetStream driver，提供無縫的 `go-fx` 整合與可靠的訊息處理。

[English Documentation](README.md)

## 設計理念

我們相信訊息基礎設施應該是可靠、透明且易於管理的。此 driver 建立在三個核心支柱之上：

1.  **Context 感知 (Context-Awareness)**：所有操作都遵循 `context.Context`。這確保您的應用程式能正確處理超時、取消和優雅關機，這對微服務至關重要。
2.  **明確的生命週期 (Explicit Lifecycle)**：我們將 Broker 視為受生命週期管理的組件。連線與斷線是明確的動作，使其非常適合與 `uber-go/fx` 等依賴注入框架一起使用。
3.  **可靠性優先 (Reliability First)**：預設採用 **Pull Consumers** 和 **同步發布 (Synchronous Publishing)**，保證更好的流量控制和資料持久性。我們優先考慮「至少一次 (at-least-once)」的傳遞保證，而非單純的射後不理 (fire-and-forget)。

## 運作原理

### 拉取式消費 (Pull-Based Consumption)
此 driver 不讓 NATS 盲目地將訊息推送到您的服務，而是使用 **Pull Consumers**。
- **批次處理**：Worker 以批次方式（可配置）拉取訊息，減少網路往返。
- **背壓 (Backpressure)**：服務只會拉取它能處理的量，防止在流量高峰時過載。
- **控制權**：您可以微調 `FetchWait` 和 `BatchSize` 來平衡延遲與吞吐量。

### 持久化消費者 (Durable Consumers)
Queue group 會自動映射到 JetStream 的 **Durable Consumers**。
- **負載平衡**：使用相同 `queue` 名稱的多個服務實例將分擔負載。
- **持久性**：NATS 會記住消費者的狀態。即使所有實例重啟，處理也會從中斷的地方準確恢復。

### 同步發布 (Synchronous Publishing)
`Publish` 方法會等待 NATS 伺服器的確認 (ACK) 後才返回。這確保當您的函數返回時，資料已安全存儲在 Stream 中。

## 使用方式

### 1. 配置 (`Broker`)
`Broker` 是核心入口點。它需要 NATS 位址，並提供效能調優選項。

```go
b := jetstream.NewBroker(
    jetstream.WithAddrs("nats://localhost:4222"),
    jetstream.WithBatchSize(10), // 優化批次處理
)
```

### 2. 生命週期管理
連線與斷線應與應用程式的生命週期綁定。

```go
// 啟動時
if err := b.Connect(ctx); err != nil {
    log.Fatal(err)
}

// 關閉時
if err := b.Disconnect(ctx); err != nil {
    log.Println("斷線時發生錯誤:", err)
}
```

### 3. 發布訊息
發布嚴格採用同步模式，以確保資料安全。

```go
msg := &driver.Message{Body: []byte("payload")}
err := b.Publish(ctx, "orders.created", msg)
```

### 4. 訂閱訊息
訂閱需要 **主題 (Subject)** 和 **佇列名稱 (Queue Name)** (即 Durable Consumer)。

```go
handler := func(ctx context.Context, msg *driver.Message) error {
    // 處理訊息...
    return nil // 返回 nil 表示 ACK。返回錯誤則會 NAK。
}

// "orders.created" -> Subject
// "order-processor" -> Durable Consumer Name (Queue)
sub, err := b.Subscribe(ctx, "orders.created", handler, driver.WithQueue("order-processor"))
```

## 架構

此 driver 實作了一個清晰的介面，將您的業務邏輯與底層 NATS 實作解耦。

- **`pkg/driver`**：定義乾淨、無依賴的介面 (`Broker`, `Subscriber`, `Message`)。
- **`pkg/driver/jetstream`**：使用官方 NATS Go client 的具體實作。

這種分離使得您可以輕鬆 mock 和測試您的業務邏輯，而無需運行的 NATS 伺服器。
