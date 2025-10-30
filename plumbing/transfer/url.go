package transfer

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"runtime"
	"strings"

	giturl "github.com/go-git/go-git/v6/internal/url"
	"github.com/go-git/go-git/v6/plumbing"
)

// ParseURL parses a Git endpoint URL, supporting scp-like syntax and file
// paths.
func ParseURL(endpoint string) (*url.URL, error) {
	if e, ok := parseSCPLike(endpoint); ok {
		return e, nil
	}

	if e, ok := parseFile(endpoint); ok {
		return e, nil
	}

	return parseURL(endpoint)
}

var fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)

func parseURL(endpoint string) (*url.URL, error) {
	if after, ok := strings.CutPrefix(endpoint, "file://"); ok {
		endpoint = after

		// When triple / is used, the path in Windows may end up having an
		// additional / resulting in "/C:/Dir".
		if runtime.GOOS == "windows" &&
			fileIssueWindows.MatchString(endpoint) {
			endpoint = endpoint[1:]
		}
		return &url.URL{
			Scheme: "file",
			Path:   endpoint,
		}, nil
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	if !u.IsAbs() {
		return nil, plumbing.NewPermanentError(fmt.Errorf(
			"invalid endpoint: %s", endpoint,
		))
	}

	return u, nil
}

func parseSCPLike(endpoint string) (*url.URL, bool) {
	if giturl.MatchesScheme(endpoint) || !giturl.MatchesScpLike(endpoint) {
		return nil, false
	}

	user, host, port, path := giturl.FindScpLikeComponents(endpoint)
	if port != "" {
		host = net.JoinHostPort(host, port)
	}

	return &url.URL{
		Scheme: "ssh",
		User:   url.User(user),
		Host:   host,
		Path:   path,
	}, true
}

func parseFile(endpoint string) (*url.URL, bool) {
	if giturl.MatchesScheme(endpoint) {
		return nil, false
	}

	path := endpoint
	return &url.URL{
		Scheme: "file",
		Path:   path,
	}, true
}
