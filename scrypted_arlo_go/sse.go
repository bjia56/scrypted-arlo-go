package scrypted_arlo_go

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/tmaxmax/go-sse"
)

type SSEClient struct {
	UUID string

	url     string
	headers HeadersMap

	messages chan sse.Event

	ctxLock *sync.Mutex
	ctx     context.Context
	conn    *sse.Connection
	cancel  context.CancelFunc
}

func NewSSEClient(url string, headers HeadersMap) (*SSEClient, error) {
	s := &SSEClient{
		UUID:     uuid.New().String(),
		url:      url,
		headers:  headers,
		messages: make(chan sse.Event),
		ctxLock:  &sync.Mutex{},
	}
	s.ctxLock.Lock()
	defer s.ctxLock.Unlock()
	return s, s.initialize()
}

// must hold ctxLock when calling this
func (s *SSEClient) initialize() error {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.url, http.NoBody)
	if err != nil {
		s.cancel()
		return err
	}

	req.Header = s.headers.toHTTPHeaders()
	s.conn = sse.NewConnection(req)
	s.conn.SubscribeToAll(func(event sse.Event) {
		s.ctxLock.Lock()
		defer s.ctxLock.Unlock()
		if s.ctx.Err() != context.Canceled {
			s.messages <- event
		}
	})

	return nil
}

func (s *SSEClient) Start() {
	go func() {
		for {
			fmt.Printf("[Arlo]: SSEClient %s starting\n", s.UUID)
			err := s.conn.Connect()
			s.ctxLock.Lock()
			if errors.Is(err, context.Canceled) || s.ctx.Err() == context.Canceled {
				fmt.Printf("[Arlo]: SSEClient %s exited\n", s.UUID)
				s.ctxLock.Unlock()
				break
			}
			fmt.Printf("[Arlo]: SSEClient %s restarting due to: %s\n", s.UUID, err)
			if err := s.initialize(); err != nil {
				fmt.Printf("[Arlo]: SSEClient %s could not be reinitialized: %v\n", s.UUID, err)
				s.ctxLock.Unlock()
				break
			}
			s.ctxLock.Unlock()
		}
		close(s.messages)
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
	s.ctxLock.Lock()
	defer s.ctxLock.Unlock()
	s.cancel()
}
