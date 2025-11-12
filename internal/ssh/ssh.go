package ssh

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"reflect"

	"github.com/go-git/go-git/v6/utils/trace"
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// NewKnownHostsDB creates a new [knownhosts.HostKeyDB] from the given files.
func NewKnownHostsDB(files ...string) (*knownhosts.HostKeyDB, error) {
	var err error
	if len(files) == 0 {
		if files, err = getDefaultKnownHostsFiles(); err != nil {
			return nil, err
		}
	}
	trace.SSH.Printf("ssh: known_hosts sources %s", files)

	if files, err = filterKnownHostsFiles(files...); err != nil {
		return nil, err
	}
	trace.SSH.Printf("ssh: filtered known_hosts sources %s", files)

	return knownhosts.NewDB(files...)
}

func getDefaultKnownHostsFiles() ([]string, error) {
	files := filepath.SplitList(os.Getenv("SSH_KNOWN_HOSTS"))
	if len(files) != 0 {
		trace.SSH.Printf("ssh: loading known_hosts from SSH_KNOWN_HOSTS")
		return files, nil
	}

	homeDirPath, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	return []string{
		filepath.Join(homeDirPath, "/.ssh/known_hosts"),
		"/etc/ssh/ssh_known_hosts",
	}, nil
}

func filterKnownHostsFiles(files ...string) ([]string, error) {
	var out []string
	for _, file := range files {
		_, err := os.Stat(file)
		if err == nil {
			out = append(out, file)
			continue
		}

		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("unable to find any valid known_hosts file, set SSH_KNOWN_HOSTS env variable")
	}

	return out, nil
}

func OverrideConfig(overrides *ssh.ClientConfig, c *ssh.ClientConfig) {
	if overrides == nil {
		return
	}

	t := reflect.TypeOf(*c)
	vc := reflect.ValueOf(c).Elem()
	vo := reflect.ValueOf(overrides).Elem()

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		vcf := vc.FieldByName(f.Name)
		vof := vo.FieldByName(f.Name)
		vcf.Set(vof)
	}

	*c = vc.Interface().(ssh.ClientConfig)
}

func TracePublicKeysCallback(getSigners func() ([]ssh.Signer, error)) ssh.AuthMethod {
	signers, err := getSigners()
	if err != nil {
		trace.SSH.Printf("ssh: error calling getSigners: %v", err)
	}
	if len(signers) == 0 {
		trace.SSH.Printf("ssh: no signers found")
	}
	for _, s := range signers {
		trace.SSH.Printf("ssh: found key: %s %s", s.PublicKey().Type(),
			ssh.FingerprintSHA256(s.PublicKey()))
	}

	cb := func() ([]ssh.Signer, error) {
		return signers, err
	}
	return ssh.PublicKeysCallback(cb)
}

func OsUsername() (string, error) {
	var username string
	if user, err := user.Current(); err == nil {
		username = user.Username
		trace.SSH.Printf("ssh: Falling back to current user name %q", username)
	} else {
		username = os.Getenv("USER")
		trace.SSH.Printf("ssh: Falling back to environment variable USER %q", username)
	}

	if username == "" {
		return "", errors.New("failed to get username")
	}

	return username, nil
}
