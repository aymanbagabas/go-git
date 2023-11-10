package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/server"
)

type CmdReceivePack struct {
	cmd

	Args struct {
		GitDir string `positional-arg-name:"git-dir" required:"true"`
	} `positional-args:"yes"`
}

func (CmdReceivePack) Usage() string {
	//TODO: git-receive-pack returns error code 129 if arguments are invalid.
	return fmt.Sprintf("usage: %s <git-dir>", os.Args[0])
}

func (c *CmdReceivePack) Execute(args []string) error {
	gitDir, err := filepath.Abs(c.Args.GitDir)
	if err != nil {
		return err
	}

	repo, err := git.PlainOpen(gitDir)
	if err != nil {
		return err
	}

	if err := server.ServeReceivePack(srvCmd, repo.Storer); err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(128)
	}

	return nil
}
