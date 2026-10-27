// Package transport carries the messages of package protocol between the two machines of a Remote Work session (SPEC §10.18
// "As built (step 15.3)"): over QUIC on UDP, or, when UDP does not get through, over the SSH channel the login opened.
//
// Both transports give the same thing, a Conn:
//
//   - a reliable, ordered path for everything but pictures (input, clipboard, notifications, control): a QUIC stream, or the
//     SSH channel, with a bounded queue in front of it (Conn.Send);
//   - a best-effort path for the tiles of a picture (Conn.SendTiles): QUIC datagrams, where a tile that does not fit one
//     datagram is cut into fragments and counts only when all of them arrive, a picture that is not the newest is dropped on
//     both sides and never queued, and the client asks for the tiles of the newest picture that did not arrive
//     (protocol.Missing); over the SSH channel the tiles are messages, still with only the newest picture waiting;
//   - one Next that gives the messages that arrived, tiles included.
//
// The host side (Host, HostConn) is a QUIC listener on the same port number as the SSH server, with a certificate made in memory
// at start, that admits a connection only when its first stream presents the token of a live session. The client side
// (Connect, ClientConn) accepts only the certificate whose SHA-256 fingerprint the SSH channel gave it, tries QUIC for three
// seconds, falls back to the SSH channel, and resumes a dropped QUIC connection with the token while the SSH connection
// lives. Presenter and Screen are the picture flow on top of a Conn: the differ of package tiles, the window of pictures in
// flight, the retry and the periodic full refresh on the host; the canvas and the confirmations on the client.
//
// The package imports package protocol (and tiles and login for the picture flow and FromLogin), so it knows nothing of
// the hub or the workspace.
package transport
