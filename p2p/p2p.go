// Package p2p implements qri peer-to-peer communication.
package p2p

import (
	"fmt"

	golog "github.com/ipfs/go-log"

	protocol "github.com/libp2p/go-libp2p-protocol"
	identify "github.com/libp2p/go-libp2p/p2p/protocol/identify"
)

var log = golog.Logger("qrip2p")

const (
	// QriProtocolID is the top level Protocol Identifier
	QriProtocolID = protocol.ID("/qri")
	// QriServiceTag tags the type & version of the qri service
	QriServiceTag = "qri/0.9.0"
	// default value to give qri peer connections in connmanager, one hunnit
	qriSupportValue = 100
	// qriSupportKey is the key we store the flag for qri support under in Peerstores and in ConnManager()
	qriSupportKey = "qri-support"
)

func init() {
	// golog.SetLogLevel("qrip2p", "debug")

	// ipfs core includes a client version. seems like a good idea.
	// TODO - understand where & how client versions are used
	identify.ClientVersion = QriServiceTag
}

// ErrNotConnected is for a missing required network connection
var ErrNotConnected = fmt.Errorf("no p2p connection")

// ErrQriProtocolNotSupported is returned when a connection can't be upgraded
var ErrQriProtocolNotSupported = fmt.Errorf("peer doesn't support the qri protocol")
