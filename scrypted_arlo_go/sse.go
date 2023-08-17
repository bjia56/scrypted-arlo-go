package scrypted_arlo_go

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/tmaxmax/go-sse"
)

type SSEClient struct {
	conn     *sse.Connection
	messages chan sse.Event
	cancel   context.CancelFunc
}

func NewSSEClient(url string, headers HeadersMap) (*SSEClient, error) {
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header = headers.toHTTPHeaders()

	conn := sse.NewConnection(req)
	messagesChan := make(chan sse.Event)
	conn.SubscribeToAll(func(event sse.Event) {
		if ctx.Err() != context.Canceled {
			messagesChan <- event
		}
	})

	return &SSEClient{
		conn:     conn,
		messages: messagesChan,
		cancel:   cancel,
	}, nil
}

func (s *SSEClient) Start() {
	go func() {
		err := s.conn.Connect()
		fmt.Printf("[Arlo]: SSEClient exited with: %v\n", err)
	}()
}

func (s *SSEClient) Next() (string, error) {
	event, ok := <-s.messages
	if !ok {
		return "", io.EOF
	}
	return event.Data, nil
}

func (s *SSEClient) Close() {
	s.cancel()
	close(s.messages)
}
