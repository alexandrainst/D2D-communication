package comm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"alexandra.dk/D2D_Agent/model"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	libp2ptls "github.com/libp2p/go-libp2p-tls"
	"github.com/libp2p/go-libp2p/p2p/discovery"
	"github.com/multiformats/go-multiaddr"
)

// DiscoveryInterval is how often we re-publish our mDNS records.
const DiscoveryInterval = time.Hour

// DiscoveryServiceTag is used in our mDNS advertisements to discover other chat peers.
const DiscoveryServiceTag = "D2D_Comunidad"

var channels = make(map[string]*Channel)

var SelfId peer.ID

var ps *pubsub.PubSub

//vars for registration
var myType model.AgentType
var discoveryPath string
var statePath string
var reorganizationPath string

var DiscoveryChannel = make(chan *Message, BufferSize)
var StateChannel = make(chan *Message, BufferSize)
var ReorganizationChannel = make(chan *Message, BufferSize)
var MissionChannel = make(chan *Message, BufferSize)

func InitD2DCommuncation(agentType model.AgentType) {
	myType = agentType
	ctx := context.Background()

	// Create a new libp2p host
	h, err := createHost()
	if err != nil {
		panic(err)
	}

	fmt.Println("Host id is ", h.ID())
	SelfId = h.ID()

	// create a new PubSub service using the GossipSub router
	ps, err = pubsub.NewGossipSub(ctx, h)
	if err != nil {
		panic(err)
	}

	// setup local mDNS discovery
	err = setupDiscovery(ctx, h)
	if err != nil {
		panic(err)
	}
}

func InitCommunicationType(path string, messageType MessageType) {
	if channels[path] != nil {
		//channel already created - ignoring
		log.Println("channel with path: " + path + " already in list. returning")
		return
	}
	switch messageType {
	case StateMessageType:
		statePath = path
		break
	case DiscoveryMessageType:
		discoveryPath = path
		break
	case ReorganizationMessageType:
		reorganizationPath = path
		break
	}
	ctx := context.Background()
	cr, err := JoinPath(ctx, ps, SelfId, path, messageType)
	if err != nil {
		panic(err)
	}
	go cr.readMessages()
}

func SendMission(senderId string, mission *model.Mission, channelPath string) error {

	channel := channels[channelPath]
	if channel == nil {
		return errors.New("channel not found with path: " + channelPath)
	}
	m := Message{
		MsgType:        MissionMessageType,
		MissionContent: *mission,
		SenderId:       senderId,
		SenderType:     myType,
	}

	msgBytes, err := json.Marshal(m)

	if err != nil {
		return err
	}
	channel.topic.Publish(channel.ctx, msgBytes)

	return nil
}

func SendState(state *model.State) {
	//log.Println("Stating me")
	//state channel
	channel := channels[statePath]
	m := Message{
		MsgType:      StateMessageType,
		StateContent: *state,
		SenderId:     state.ID,
		SenderType:   myType,
	}
	// log.Println("state msg:")
	// log.Println(m)
	msgBytes, err := json.Marshal(m)

	if err != nil {
		panic(err)
	}
	channel.topic.Publish(channel.ctx, msgBytes)

}

func AnnounceSelf(metadata *model.Agent) {
	//log.Println("Announce:")
	// log.Println(*metadata)
	//registration channel
	channel := channels[discoveryPath]

	m := Message{
		MsgType:          DiscoveryMessageType,
		DiscoveryContent: *metadata,
		SenderId:         metadata.ID,
		SenderType:       myType,
	}
	//log.Println(m)
	msgBytes, err := json.Marshal(m)

	if err != nil {
		panic(err)
	}
	channel.topic.Publish(channel.ctx, msgBytes)
}

func SendReorganization(metadata model.Agent, selfId string) {
	//log.Printf("Discovered lost agent with id %v. Notifying network \n", metadata.ID)
	// log.Println(*metadata)
	//registration channel
	channel := channels[reorganizationPath]

	m := Message{
		MsgType:          ReorganizationMessageType,
		DiscoveryContent: metadata,
		SenderId:         selfId,
		SenderType:       myType,
	}
	//log.Println(m)
	msgBytes, err := json.Marshal(m)

	if err != nil {
		panic(err)
	}
	channel.topic.Publish(channel.ctx, msgBytes)
}

func createHost() (host.Host, error) {
	// Creates a new RSA key pair for this host.
	var r = rand.Reader
	prvKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		panic(err)
	}

	//host will listens on a random local TCP port on the IPv4.
	sourceMultiAddr, _ := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/0"))

	// libp2p.New constructs a new libp2p Host.
	host, err := libp2p.New(
		context.Background(),
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(prvKey),
		// support TLS connections
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
	)

	if err != nil {
		return nil, err
	}

	return host, nil
}

func (cr *Channel) readMessages() {
	log.Println("Starting cr.readMessages() with path: " + cr.path)
	for {
		message := <-cr.Messages
		if cr.Close {
			log.Println("closing channel: " + cr.path)
			return
		}
		switch cr.roomType {
		case DiscoveryMessageType:
			DiscoveryChannel <- message
			break
		case StateMessageType:
			StateChannel <- message
			break
		case ReorganizationMessageType:
			ReorganizationChannel <- message
			break
		case MissionMessageType:
			MissionChannel <- message
			break
		}
	}
}

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	h host.Host
}

// HandlePeerFound connects to peers discovered via mDNS. Once they're connected,
// the PubSub system will automatically start interacting with them if they also
// support PubSub.
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	fmt.Printf("discovered new peer %s\n", pi.ID.Pretty())
	err := n.h.Connect(context.Background(), pi)
	if err != nil {
		fmt.Printf("error connecting to peer %s: %s\n", pi.ID.Pretty(), err)
	}
}

// setupDiscovery creates an mDNS discovery service and attaches it to the libp2p Host.
// This lets us automatically discover peers on the same LAN and connect to them.
func setupDiscovery(ctx context.Context, h host.Host) error {
	// setup mDNS discovery to find local peers
	disc, err := discovery.NewMdnsService(ctx, h, DiscoveryInterval, DiscoveryServiceTag)
	if err != nil {
		return err
	}

	n := discoveryNotifee{h: h}
	disc.RegisterNotifee(&n)
	return nil
}
