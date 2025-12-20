# NATS JetStream Driver for Go

為 `go-fx` 整合設計的生產就緒 NATS JetStream driver，提供可靠的訊息發布和基於拉取的消費機制。

[English Documentation](README.md)

## 概述

此 driver 將 NATS JetStream 連接到您的 Go 應用程式，實現可靠的事件驅動微服務通訊。它採用批次處理的拉取式消費者，提供最佳吞吐量和背壓管理，並暴露符合現代 Go 實踐的 Context 感知介面。

## 主要特性

- **Context 感知介面**：所有方法（`Connect`、`Publish`、`Subscribe`）都接受 `context.Context` 用於取消和追蹤。
- **go-fx 整合**：專為與 `go.uber.org/fx` 無縫協作而設計，便於生命週期管理。
- **拉取式訂閱**：背景工作程式以可配置的批次大小擷取訊息。
- **同步發布**：等待 JetStream 確認以保證持久化。
- **自動建立串流**：首次發布/訂閱時自動建立串流，使用合理的預設值。
- **持久化消費者**：佇列名稱映射到 JetStream 持久化消費者以實現負載平衡。

## 安裝

```bash
go get go-micro-jetstream
```

### 依賴項
- `github.com/nats-io/nats.go` - NATS JetStream 客戶端
- `go.uber.org/fx` - 依賴注入框架（可選但推薦）

## 快速開始

### 1. 啟動 NATS 並啟用 JetStream

```bash
docker run -d --name nats-jetstream \
  -p 4222:4222 -p 8222:8222 \
  nats:latest -js -m 8222
```

### 2. 定義應用程式 (使用 go-fx)

```go
package main

import (
	"context"
	"log"
	"time"

	"go.uber.org/fx"
	"go-micro-jetstream/pkg/driver"
	"go-micro-jetstream/pkg/driver/jetstream"
)

func main() {
	app := fx.New(
		fx.Provide(NewBroker),
		fx.Invoke(SetupSubscriber),
		fx.Invoke(PublishMessages),
	)
	app.Run()
}
```

### 3. 建立 Broker Provider

```go
func NewBroker(lc fx.Lifecycle) (driver.Broker, error) {
	b := jetstream.NewBroker(
		jetstream.WithAddrs("localhost:4222"),
		jetstream.WithBatchSize(10),
		jetstream.WithFetchWait(5*time.Second),
		jetstream.WithClientName("my-service"),
        // 可選：注入自訂 Logger
        // jetstream.WithLogger(myLogger),
	)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Println("正在連接 broker...")
			return b.Connect(ctx)
		},
		OnStop: func(ctx context.Context) error {
			log.Println("正在斷開 broker 連接...")
			return b.Disconnect(ctx)
		},
	})

	return b, nil
}
```

### 4. 訂閱訊息

```go
func SetupSubscriber(lc fx.Lifecycle, b driver.Broker) {
    var sub driver.Subscriber

    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            handler := func(ctx context.Context, msg *driver.Message) error {
                log.Printf("收到訊息: %s", string(msg.Body))
                return nil // 成功時 Ack，返回錯誤則 Nak
            }

            s, err := b.Subscribe(
                ctx,
                "orders.created",
                handler,
                driver.WithQueue("order-processor"), // 必需：持久化消費者名稱
            )
            if err != nil {
                return err
            }
            sub = s
            return nil
        },
        OnStop: func(ctx context.Context) error {
            if sub != nil {
                return sub.Unsubscribe(ctx)
            }
            return nil
        },
    })
}
```

### 5. 發布訊息

```go
func PublishMessages(lc fx.Lifecycle, b driver.Broker) {
    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            go func() {
                msg := &driver.Message{
                    Body: []byte(`{"id": 123}`),
                }
                if err := b.Publish(context.Background(), "orders.created", msg); err != nil {
                    log.Printf("發布失敗: %v", err)
                }
            }()
            return nil
        },
    })
}
```

## 配置選項

### Broker 選項 (`pkg/driver/jetstream`)

```go
// NATS 伺服器位址
jetstream.WithAddrs("nats://localhost:4222")

// Fetch 操作的批次大小（預設：10）
jetstream.WithBatchSize(20)

// Fetch 超時時間（預設：5 秒）
jetstream.WithFetchWait(3 * time.Second)

// NATS 客戶端名稱（預設：go-micro-{hostname}）
jetstream.WithClientName("my-service")

// 自訂 Logger
jetstream.WithLogger(myLogger)

// 自訂 NATS 選項，用於 TLS/認證
jetstream.WithNatsOptions(
    nats.UserInfo("user", "password"),
)
```

### 訂閱選項 (`pkg/driver`)

```go
// 持久化消費者名稱（必需）
driver.WithQueue("my-consumer-group")
```

## 架構

此 driver 實作了位於 `pkg/driver/broker.go` 中的清晰 `driver.Broker` 介面。實作位於 `pkg/driver/jetstream`。

- **訂閱**：每個訂閱執行一個專用的 goroutine 從 JetStream 拉取訊息。
- **Panic 恢復**：處理器受到 panic 恢復保護，防止工作程式崩潰。
- **連線管理**：連線邏輯與實例建立分離，允許透過依賴注入生命週期進行更好的控制。

## 與舊版 `go-micro` Broker 的變更

- **套件**：從 `pkg/broker` 移動到 `pkg/driver` + `pkg/driver/jetstream`。
- **介面**：所有方法使用 `context.Context`。
- **依賴**：不再依賴 `go-micro.dev/v5`。
- **日誌**：使用標準 `log` 或注入的 logger 代替 `go-micro/logger`。
