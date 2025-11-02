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
func ParseURL(rawURL string) (*url.URL, error) {
	if e, ok := parseSCPLike(rawURL); ok {
		return e, nil
	}

	if e, ok := parseFile(rawURL); ok {
		return e, nil
	}

	return parseURL(rawURL)
}

var fileIssueWindows = regexp.MustCompile(`^/[A-Za-z]:(/|\\)`)

func parseURL(rawURL string) (*url.URL, error) {
	if after, ok := strings.CutPrefix(rawURL, "file://"); ok {
		rawURL = after

		// When triple / is used, the path in Windows may end up having an
		// additional / resulting in "/C:/Dir".
		if runtime.GOOS == "windows" &&
			fileIssueWindows.MatchString(rawURL) {
			rawURL = rawURL[1:]
		}
		return &url.URL{
			Scheme: "file",
			Path:   rawURL,
		}, nil
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	if !u.IsAbs() {
		return nil, plumbing.NewPermanentError(fmt.Errorf(
			"invalid endpoint: %s", rawURL,
		))
	}

	return u, nil
}

func parseSCPLike(rawURL string) (*url.URL, bool) {
	if giturl.MatchesScheme(rawURL) || !giturl.MatchesScpLike(rawURL) {
		return nil, false
	}

	user, host, port, path := giturl.FindScpLikeComponents(rawURL)
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

func parseFile(rawURL string) (*url.URL, bool) {
	if giturl.MatchesScheme(rawURL) {
		return nil, false
	}

	path := rawURL
	return &url.URL{
		Scheme: "file",
		Path:   path,
	}, true
}
