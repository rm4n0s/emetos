package app

import (
	"crypto/tls"
	"net/http"
	"net/rpc"
)

var _ = http.Get
var _ = rpc.Dial
var _ = tls.Dial
