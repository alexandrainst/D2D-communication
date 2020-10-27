package comm

import (
	"context"
	"encoding/json"
	"log"

	"alexandra.dk/D2D_Agent/model"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

// BufferSize is the number of incoming messages to buffer for each topic.
const BufferSize = 128

type MessageType int

const (
	DiscoveryMessageType      = 0
	StateMessageType          = 1
	MissionMessageType        = 2
	ReorganizationMessageType = 3
	RecalculatorMessageType   = 4
)

// Channel represents a subscription to a single PubSub topic. Messages
// can be published to the topic with Channel.Publish, and received
// messages are pushed to the Messages channel.
type Channel struct {
	// Messages is a channel of messages received from other peers in the chat room
	Messages chan *Message

	ctx      context.Context
	ps       *pubsub.PubSub
	topic    *pubsub.Topic
	sub      *pubsub.Subscription
	roomType MessageType
	path     string
	self     peer.ID
	Close    bool
}

// Message gets converted to/from JSON and sent in the body of pubsub messages.
type Message struct {
	MsgType          MessageType
	DiscoveryContent model.Agent
	StateContent     model.State
	MissionContent   model.Mission
	SenderId         string
	SenderType       model.AgentType
}

func JoinPath(ctx context.Context, ps *pubsub.PubSub, selfID peer.ID, path string, roomType MessageType) (*Channel, error) {
	topic, err := ps.Join(path)
	if err != nil {
		return nil, err
	}

	// and subscribe to it
	sub, err := topic.Subscribe()

	if err != nil {
		return nil, err
	}

	ch := &Channel{
		ctx:      ctx,
		ps:       ps,
		topic:    topic,
		sub:      sub,
		self:     selfID,
		path:     path,
		roomType: roomType,
		Close:    false,
		Messages: make(chan *Message, BufferSize),
	}
	channels[path] = ch
	// start reading messages from the subscription in a loop
	log.Println("Channel with path " + path + " joined, read loop started")
	go ch.readLoop()
	return ch, nil
}

func ClosePath(disconnectedAgent model.Agent, messageType MessageType) {
	var removeId string
	switch messageType {
	case MissionMessageType:
		removeId = disconnectedAgent.ID
	}
	removeChannel := channels[removeId]
	if removeChannel == nil {
		log.Println("Trying to close a channel with id: " + removeId + ", but channel does not exist. Returning")
		return
	}
	removeChannel.Close = true

	removeChannel.sub.Cancel()
	removeChannel.topic.Close()
	delete(channels, removeId)
}

// readLoop pulls messages from the pubsub topic and pushes them onto the Messages channel.
func (cr *Channel) readLoop() {
	for {

		msg, err := cr.sub.Next(cr.ctx)
		if err != nil {
			close(cr.Messages)
			return
		}
		// only forward messages delivered by others
		if msg.ReceivedFrom == cr.self {
			continue
		}
		cm := new(Message)
		err = json.Unmarshal(msg.Data, cm)
		if err != nil {
			log.Println("eeerrrr")
			log.Println(err)
			continue
		}
		//log.Println("messages received")
		// send valid messages onto the Messages channel

		cr.Messages <- cm
	}
}
