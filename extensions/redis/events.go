package redis

import (
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The events of the extension (SPEC §10.23). They carry no keys, no values and no command text: only which connection, and whether and
// how fast a command went.
const (
	// TopicConnected and TopicDisconnected are published when a connection opens and closes. Payload ConnectionEvent.
	TopicConnected    = ID + ".connected"
	TopicDisconnected = ID + ".disconnected"
	// TopicCommandFinished is published when a command of the console or of a .redis file ends. Payload CommandFinished.
	TopicCommandFinished = ID + ".commandFinished"
)

// ConnectionEvent is the payload of TopicConnected and TopicDisconnected.
type ConnectionEvent struct {
	// Connection is the id of the connection in the Databases view, and Name its name.
	Connection string `json:"connection"`
	Name       string `json:"name"`
}

// CommandFinished is the payload of TopicCommandFinished.
type CommandFinished struct {
	// Connection is the id of the connection the command ran on.
	Connection string `json:"connection"`
	// OK is false when the server answered with an error or the command could not be sent.
	OK bool `json:"ok"`
	// Millis is how long the command took.
	Millis int64 `json:"millis"`
}

// publish publishes an event on the UI thread. It does nothing after the activation ended.
func (x *Extension) publish(topic string, payload any) {
	if x.ctx == nil || x.ctx.Err() != nil {
		return
	}
	x.api.Post(func() {
		if x.ctx.Err() != nil {
			return
		}
		if err := x.api.Events().Publish(topic, payload); err != nil {
			x.api.Log().Warn("an event cannot be published", "topic", topic, "err", err)
		}
	})
}

// publishConnection publishes that a connection opened or closed.
func (x *Extension) publishConnection(topic string, c dbconn.Connection) {
	x.publish(topic, ConnectionEvent{Connection: c.ID, Name: c.Name})
}

// publishCommandFinished publishes that a command of the console ended.
func (x *Extension) publishCommandFinished(conn string, ok bool, millis int64) {
	x.publish(TopicCommandFinished, CommandFinished{Connection: conn, OK: ok, Millis: millis})
}
