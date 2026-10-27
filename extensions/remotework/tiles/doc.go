// Package tiles cuts the picture of a view into 64x64 tiles, sends the ones that changed since the picture the client
// confirmed, and puts them together again on the client (SPEC §10.18 "As built (step 15.1)").
//
// The host side is a Differ: it is given each new picture, compares it tile by tile with the last picture the client
// confirmed (by a hash of each tile, so it keeps no pictures, only a few kilobytes of hashes), and makes the Tile messages
// of protocol for the tiles that differ, each deflated on its own so that a lost one costs nothing else. The client side
// is a Canvas: it applies the tiles, shows the picture while it is still arriving, and says when a whole picture is there
// (the moment to send protocol.FrameConfirm).
//
// The two sides stay consistent when tiles are lost, repeated or arrive late:
//
//   - a picture is always the changes to the confirmed one (Tile.Base), never to a picture the client may not have, so a
//     lost tile is sent again by the next picture, which has every tile that differs from the confirmed one;
//   - a picture that is not the newest is dropped, so the client never goes back;
//   - when the newest picture does not touch a tile an older, unfinished one changed, the client puts the confirmed tile
//     back when the newest is complete;
//   - a confirm the host does not know makes it send the whole picture again.
//
// Pixels are those of image.RGBA (R, G, B, A in this order, one byte each) with the alpha of the view, which is opaque.
package tiles
