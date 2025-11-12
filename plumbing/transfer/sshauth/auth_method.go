package sshauth

import (
	"errors"
	"fmt"
	"net"
	"os"

	sshinternal "github.com/go-git/go-git/v6/internal/ssh"
	"github.com/go-git/go-git/v6/plumbing/transfer"
	"github.com/go-git/go-git/v6/utils/trace"
	sshagent "github.com/xanzy/ssh-agent"

	"golang.org/x/crypto/ssh"
)

// ErrNotSSHTransport is returned when an authentication method is used with a
// non-SSH transport.
var ErrNotSSHTransport = errors.New("auth method requires SSH transport")

// DefaultUsername is the default username used when no username is provided.
const DefaultUsername = "git"

// authMethod is the interface all auth methods for the ssh client
// must implement. The clientConfig method returns the ssh client
// configuration needed to establish an ssh connection.
type authMethod interface {
	transfer.AuthMethod
	// ClientConfig should return a valid ssh.ClientConfig to be used to create
	// a connection to the SSH server.
	ClientConfig() (*ssh.ClientConfig, error)
}

// The names of the AuthMethod implementations. To be returned by the
// Name() method. Most git servers only allow PublicKeysName and
// PublicKeysCallbackName.
const (
	KeyboardInteractiveName = "ssh-keyboard-interactive"
	PasswordName            = "ssh-password"
	PasswordCallbackName    = "ssh-password-callback"
	PublicKeysName          = "ssh-public-keys"
	PublicKeysCallbackName  = "ssh-public-key-callback"
)

// KeyboardInteractive implements AuthMethod by using a
// prompt/response sequence controlled by the server.
type KeyboardInteractive struct {
	User      string
	Challenge ssh.KeyboardInteractiveChallenge
	HostKeyCallbackHelper
}

// SetAuth sets the transport authentication method to use keyboard-interactive.
func (a *KeyboardInteractive) SetAuth(t transfer.TransportOld) error {
	return setSSHAuth(t, a)
}

func (a *KeyboardInteractive) Name() string {
	return KeyboardInteractiveName
}

func (a *KeyboardInteractive) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *KeyboardInteractive) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", KeyboardInteractiveName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{
			a.Challenge,
		},
	})
}

// Password implements AuthMethod by using the given password.
type Password struct {
	User     string
	Password string
	HostKeyCallbackHelper
}

// SetAuth sets the transport authentication method to use password.
func (a *Password) SetAuth(t transfer.TransportOld) error {
	return setSSHAuth(t, a)
}

func (a *Password) Name() string {
	return PasswordName
}

func (a *Password) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *Password) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", PasswordName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.Password(a.Password)},
	})
}

// PasswordCallback implements AuthMethod by using a callback
// to fetch the password.
type PasswordCallback struct {
	User     string
	Callback func() (pass string, err error)
	HostKeyCallbackHelper
}

// SetAuth sets the transport authentication method to use password-callback.
func (a *PasswordCallback) SetAuth(t transfer.TransportOld) error {
	return setSSHAuth(t, a)
}

func (a *PasswordCallback) Name() string {
	return PasswordCallbackName
}

func (a *PasswordCallback) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PasswordCallback) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", PasswordCallbackName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.PasswordCallback(a.Callback)},
	})
}

// PublicKeys implements AuthMethod by using the given key pairs.
type PublicKeys struct {
	User   string
	Signer ssh.Signer
	HostKeyCallbackHelper
}

// NewPublicKeys returns a PublicKeys from a PEM encoded private key. An
// encryption password should be given if the pemBytes contains a password
// encrypted PEM block otherwise password should be empty. It supports RSA
// (PKCS#1), PKCS#8, DSA (OpenSSL), and ECDSA private keys.
func NewPublicKeys(user string, pemBytes []byte, password string) (*PublicKeys, error) {
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if _, ok := err.(*ssh.PassphraseMissingError); ok {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(password))
	}
	if err != nil {
		return nil, err
	}
	return &PublicKeys{User: user, Signer: signer}, nil
}

// NewPublicKeysFromFile returns a PublicKeys from a file containing a PEM
// encoded private key. An encryption password should be given if the pemBytes
// contains a password encrypted PEM block otherwise password should be empty.
func NewPublicKeysFromFile(user, pemFile, password string) (*PublicKeys, error) {
	bytes, err := os.ReadFile(pemFile)
	if err != nil {
		return nil, err
	}

	return NewPublicKeys(user, bytes, password)
}

// SetAuth sets the transport authentication method to use public-keys.
func (a *PublicKeys) SetAuth(t transfer.TransportOld) error {
	return setSSHAuth(t, a)
}

func (a *PublicKeys) Name() string {
	return PublicKeysName
}

func (a *PublicKeys) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PublicKeys) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s signer=\"%s %s\"", PublicKeysName, a.User,
		a.Signer.PublicKey().Type(),
		ssh.FingerprintSHA256(a.Signer.PublicKey()))
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(a.Signer)},
	})
}

// PublicKeysCallback implements AuthMethod by asking a
// ssh.agent.Agent to act as a signer.
type PublicKeysCallback struct {
	User     string
	Callback func() (signers []ssh.Signer, err error)
	HostKeyCallbackHelper
}

// NewSSHAgentAuth returns a PublicKeysCallback based on a SSH agent, it opens
// a pipe with the SSH agent and uses the pipe as the implementer of the public
// key callback function.
func NewSSHAgentAuth(u string) (*PublicKeysCallback, error) {
	var err error
	if u == "" {
		u, err = sshinternal.OsUsername()
		if err != nil {
			return nil, err
		}
	}

	a, _, err := sshagent.New()
	if err != nil {
		return nil, fmt.Errorf("error creating SSH agent: %q", err)
	}

	return &PublicKeysCallback{
		User:     u,
		Callback: a.Signers,
	}, nil
}

// SetAuth sets the transport authentication method to use public-keys-callback.
func (a *PublicKeysCallback) SetAuth(t transfer.TransportOld) error {
	return setSSHAuth(t, a)
}

func (a *PublicKeysCallback) Name() string {
	return PublicKeysCallbackName
}

func (a *PublicKeysCallback) String() string {
	return fmt.Sprintf("user: %s, name: %s", a.User, a.Name())
}

func (a *PublicKeysCallback) ClientConfig() (*ssh.ClientConfig, error) {
	trace.SSH.Printf("ssh: %s user=%s", PublicKeysCallbackName, a.User)
	return a.SetHostKeyCallback(&ssh.ClientConfig{
		User: a.User,
		Auth: []ssh.AuthMethod{sshinternal.TracePublicKeysCallback(a.Callback)},
	})
}

// NewKnownHostsCallback returns ssh.HostKeyCallback based on a file based on a
// known_hosts file. http://man.openbsd.org/sshd#SSH_KNOWN_HOSTS_FILE_FORMAT
//
// If list of files is empty, then it will be read from the SSH_KNOWN_HOSTS
// environment variable, example:
//
//	/home/foo/custom_known_hosts_file:/etc/custom_known/hosts_file
//
// If SSH_KNOWN_HOSTS is not set the following file locations will be used:
//
//	~/.ssh/known_hosts
//	/etc/ssh/ssh_known_hosts
func NewKnownHostsCallback(files ...string) (ssh.HostKeyCallback, error) {
	db, err := sshinternal.NewKnownHostsDB(files...)
	return db.HostKeyCallback(), err
}

// HostKeyCallbackHelper is a helper that provides common functionality to
// configure HostKeyCallback into a ssh.ClientConfig.
type HostKeyCallbackHelper struct {
	// HostKeyCallback is the function type used for verifying server keys.
	// If nil default callback will be create using NewKnownHostsCallback
	// without argument.
	HostKeyCallback ssh.HostKeyCallback
}

// SetHostKeyCallback sets the field HostKeyCallback in the given cfg. If
// HostKeyCallback is empty a default callback is created using
// NewKnownHostsCallback.
func (m *HostKeyCallbackHelper) SetHostKeyCallback(cfg *ssh.ClientConfig) (*ssh.ClientConfig, error) {
	if m.HostKeyCallback == nil {
		db, err := sshinternal.NewKnownHostsDB()
		if err != nil {
			return cfg, err
		}
		m.HostKeyCallback = db.HostKeyCallback()
		trace.SSH.Printf("ssh: no HostKeyCallback provided, using known_hosts file")
	}

	cfg.HostKeyCallback = m.traceHostKeyCallback
	return cfg, nil
}

func (m *HostKeyCallbackHelper) traceHostKeyCallback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	trace.SSH.Printf(
		`ssh: hostkey callback hostname=%s remote=%s pubkey="%s %s"`,
		hostname, remote, key.Type(), ssh.FingerprintSHA256(key))
	return m.HostKeyCallback(hostname, remote, key)
}

func setSSHAuth(t transfer.TransportOld, a authMethod) error {
	if st, ok := t.(*transfer.SSHTransport); ok {
		cc, err := a.ClientConfig()
		if err != nil {
			return err
		}
		trace.SSH.Printf("ssh: overriding transport ClientConfig with auth method %s", a.Name())
		sshinternal.OverrideConfig(cc, st.ClientConfig)
		return nil
	}
	return ErrNotSSHTransport
}
