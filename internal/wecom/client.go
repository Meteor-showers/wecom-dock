package wecom

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultEndpoint = "wss://openws.work.weixin.qq.com"
	cmdSubscribe    = "aibot_subscribe"
	cmdPing         = "ping"
	cmdMsg          = "aibot_msg_callback"
	cmdEvent        = "aibot_event_callback"
	cmdRespond      = "aibot_respond_msg"
	cmdWelcome      = "aibot_respond_welcome_msg"
	cmdUpdateCard   = "aibot_respond_update_msg"
	cmdSend         = "aibot_send_msg"
)

// Handler receives messages. Callbacks run on the read loop and must return quickly.
type Handler interface {
	OnMessage(ctx context.Context, msg Incoming)
	OnEvent(ctx context.Context, msg Incoming)
}

// Client keeps one WeCom smart-bot long connection.
type Client struct {
	BotID    string
	Secret   string
	Endpoint string
	Handler  Handler
	Log      *slog.Logger

	mu      sync.Mutex
	writeMu sync.Mutex
	conn    *websocket.Conn
}

// Run connects, subscribes, and reconnects until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	if c.Endpoint == "" {
		c.Endpoint = defaultEndpoint
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		c.Log.Warn("企业微信连接断开，准备重连", "err", err, "wait", delay.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
		if err == nil {
			delay = time.Second
		}
	}
}

func (c *Client) session(ctx context.Context) error {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	dialer.Proxy = http.ProxyFromEnvironment
	conn, _, err := dialer.DialContext(ctx, c.Endpoint, nil)
	if err != nil {
		return err
	}
	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		c.writeMu.Lock()
		_ = conn.Close()
		c.writeMu.Unlock()
	}()
	conn.SetReadLimit(1 << 20)
	if err := c.subscribe(conn); err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	c.Log.Info("企业微信长连接已建立")

	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.heartbeat(sessCtx, conn)

	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Second))
		var frame inFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			c.Log.Warn("无法解析企业微信消息", "err", err)
			continue
		}
		if frame.ErrCode != 0 {
			c.Log.Warn("企业微信返回错误", "errcode", frame.ErrCode, "errmsg", frame.ErrMsg, "cmd", frame.Cmd, "req", frame.Headers.ReqID)
		}
		switch frame.Cmd {
		case cmdMsg:
			msg, err := parseBody(frame.Headers.ReqID, frame.Body)
			if err != nil {
				c.Log.Warn("解析消息失败", "err", err)
				continue
			}
			c.Log.Info("收到消息", "user", msg.UserID, "chat", msg.ChatID, "text", msg.Text)
			if c.Handler != nil {
				c.Handler.OnMessage(sessCtx, msg)
			}
		case cmdEvent:
			msg, err := parseBody(frame.Headers.ReqID, frame.Body)
			if err != nil {
				c.Log.Warn("解析事件失败", "err", err)
				continue
			}
			c.Log.Info("收到事件", "event", msg.Event, "user", msg.UserID, "key", msg.EventKey)
			if msg.Event == "disconnected_event" {
				return errors.New("连接被新的订阅踢掉")
			}
			if c.Handler != nil {
				c.Handler.OnEvent(sessCtx, msg)
			}
		default:
			if frame.Cmd != "" {
				c.Log.Info("收到未处理的企业微信帧", "cmd", frame.Cmd, "req", frame.Headers.ReqID)
			}
		}
	}
}

func (c *Client) subscribe(conn *websocket.Conn) error {
	req := newReqID()
	if err := writeFrame(conn, &c.writeMu, outFrame{
		Cmd:     cmdSubscribe,
		Headers: map[string]string{"req_id": req},
		Body: map[string]string{
			"bot_id": c.BotID,
			"secret": c.Secret,
		},
	}); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("读取订阅响应: %w", err)
		}
		var resp inFrame
		if err := json.Unmarshal(raw, &resp); err != nil {
			return err
		}
		if resp.Cmd != "" && resp.Headers.ReqID != req {
			continue
		}
		if resp.ErrCode != 0 {
			return fmt.Errorf("订阅失败 %d %s", resp.ErrCode, resp.ErrMsg)
		}
		_ = conn.SetReadDeadline(time.Time{})
		return nil
	}
}

func (c *Client) heartbeat(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writeFrame(conn, &c.writeMu, outFrame{
				Cmd:     cmdPing,
				Headers: map[string]string{"req_id": newReqID()},
			}); err != nil {
				return
			}
		}
	}
}

// ReplyMarkdown answers the message that produced reqID.
// Long-connection replies have to be a finished stream. A markdown body is accepted
// by the socket and then never shown in the chat.
func (c *Client) ReplyMarkdown(ctx context.Context, reqID, content string) error {
	streamID := reqID
	if streamID == "" {
		streamID = newReqID()
	}
	return c.writeCurrent(ctx, outFrame{
		Cmd:     cmdRespond,
		Headers: map[string]string{"req_id": reqID},
		Body: map[string]any{
			"msgtype": "stream",
			"stream": map[string]any{
				"id":      streamID,
				"finish":  true,
				"content": content,
			},
		},
	})
}

// ReplyWelcome answers an enter_chat event. It must be sent within 5 seconds.
func (c *Client) ReplyWelcome(ctx context.Context, reqID, content string) error {
	return c.writeCurrent(ctx, outFrame{
		Cmd:     cmdWelcome,
		Headers: map[string]string{"req_id": reqID},
		Body: map[string]any{
			"msgtype": "text",
			"text":    map[string]string{"content": content},
		},
	})
}

// UpdateCard replaces the card that produced reqID. It must be sent within 5 seconds.
func (c *Client) UpdateCard(ctx context.Context, reqID string, card Card) error {
	return c.writeCurrent(ctx, outFrame{
		Cmd:     cmdUpdateCard,
		Headers: map[string]string{"req_id": reqID},
		Body: map[string]any{
			"response_type": "update_template_card",
			"template_card": cardPayload(card),
		},
	})
}

// SendMarkdown pushes a message. The chat must have talked to the bot before.
func (c *Client) SendMarkdown(ctx context.Context, chatID string, chatType int, content string) error {
	return c.writeCurrent(ctx, outFrame{
		Cmd:     cmdSend,
		Headers: map[string]string{"req_id": newReqID()},
		Body: map[string]any{
			"chatid":    chatID,
			"chat_type": chatType,
			"msgtype":   "markdown",
			"markdown":  map[string]string{"content": content},
		},
	})
}

// SendCard pushes a button card.
func (c *Client) SendCard(ctx context.Context, chatID string, chatType int, card Card) error {
	return c.writeCurrent(ctx, outFrame{
		Cmd:     cmdSend,
		Headers: map[string]string{"req_id": newReqID()},
		Body: map[string]any{
			"chatid":        chatID,
			"chat_type":     chatType,
			"msgtype":       "template_card",
			"template_card": cardPayload(card),
		},
	})
}

func (c *Client) writeCurrent(ctx context.Context, frame outFrame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("企业微信还没连上")
	}
	return writeFrame(conn, &c.writeMu, frame)
}

type outFrame struct {
	Cmd     string            `json:"cmd,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    any               `json:"body,omitempty"`
}

type inFrame struct {
	Cmd     string          `json:"cmd"`
	Headers frameHeaders    `json:"headers"`
	Body    json.RawMessage `json:"body"`
	ErrCode int             `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
}

type frameHeaders struct {
	ReqID string `json:"req_id"`
}

func writeFrame(conn *websocket.Conn, mu *sync.Mutex, frame outFrame) error {
	mu.Lock()
	defer mu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteJSON(frame)
}

func newReqID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
