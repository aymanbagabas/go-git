// Package transport includes the implementation for different transport
// protocols.
//
// `Transport` can be used to fetch and send packfiles to a git server.
// go-git supports HTTP, SSH, Git, and file protocols, but you can also install
// your own protocols using `transport.Register`.
//
// Each protocol has its own implementation of `Transport`.
package transport
