package queue

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/puravnayak/apishield/internal/events"
)

// Publisher defines the contract for asynchronously publishing webhook tasks
type Publisher interface {
	Publish(ctx context.Context, event events.WebhookEvent) error
}

// RabbitMQPublisher implements the Publisher interface using RabbitMQ
type RabbitMQPublisher struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	queueName string
}

// NewRabbitMQPublisher opens a channel, declares the DLQ topology (exchange, queues, bindings), and returns a RabbitMQPublisher
func NewRabbitMQPublisher(conn *amqp.Connection, queueName string) (*RabbitMQPublisher, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	dlxName := "webhook_dlx"
	dlqName := "webhook_dlq"
	dlqRoutingKey := "webhook_dlq"

	// 1. Declare the Dead Letter Exchange (DLX)
	err = ch.ExchangeDeclare(
		dlxName,  // name
		"direct", // type
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	if err != nil {
		ch.Close()
		return nil, fmt.Errorf("failed to declare Dead Letter Exchange (%s): %w", dlxName, err)
	}

	// 2. Declare the Dead Letter Queue (DLQ)
	_, err = ch.QueueDeclare(
		dlqName, // name
		true,    // durable
		false,   // delete when unused
		false,   // exclusive
		false,   // no-wait
		nil,     // arguments
	)
	if err != nil {
		ch.Close()
		return nil, fmt.Errorf("failed to declare Dead Letter Queue (%s): %w", dlqName, err)
	}

	// 3. Bind the DLQ to the DLX
	err = ch.QueueBind(
		dlqName,       // queue name
		dlqRoutingKey, // routing key
		dlxName,       // exchange name
		false,         // no-wait
		nil,           // arguments
	)
	if err != nil {
		ch.Close()
		return nil, fmt.Errorf("failed to bind Dead Letter Queue to DLX: %w", err)
	}

	// 4. Declare the Main Queue with bindings routing failed/expired messages to the DLX
	args := amqp.Table{
		"x-dead-letter-exchange":    dlxName,
		"x-dead-letter-routing-key": dlqRoutingKey,
	}
	_, err = ch.QueueDeclare(
		queueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		args,      // arguments
	)
	if err != nil {
		ch.Close()
		return nil, fmt.Errorf("failed to declare main queue (%s): %w", queueName, err)
	}

	return &RabbitMQPublisher{
		conn:      conn,
		channel:   ch,
		queueName: queueName,
	}, nil
}

// Publish serializes a WebhookEvent to JSON and publishes it to the main queue
func (p *RabbitMQPublisher) Publish(ctx context.Context, event events.WebhookEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook event: %w", err)
	}

	err = p.channel.PublishWithContext(
		ctx,
		"",          // exchange (publish to the default exchange)
		p.queueName, // routing key matches the queue name
		false,       // mandatory
		false,       // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message to queue %s: %w", p.queueName, err)
	}

	return nil
}

// Close closes the active RabbitMQ channel
func (p *RabbitMQPublisher) Close() error {
	return p.channel.Close()
}
