package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
	"private-drive/internal/recovery"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Recovery error:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: harbor-recovery backup|restore|verify [options]")
	}
	command := os.Args[1]
	flags := flag.NewFlagSet("harbor-recovery "+command, flag.ContinueOnError)
	var opts recovery.BackupOptions
	var archive, destination, passwordFile string
	flags.StringVar(&passwordFile, "password-file", "", "Private file containing backup password; omit for secure terminal prompt")
	switch command {
	case "backup":
		flags.StringVar(&opts.DataDir, "data-dir", "", "Stopped Harbor data directory")
		flags.StringVar(&opts.Output, "output", "", "New encrypted archive, outside data directory")
		flags.StringVar(&opts.KeyFile, "key-file", "", "External private content key (default: data-dir/content.key)")
		flags.StringVar(&opts.OwnerPasswordFile, "owner-password-file", "", "External private owner password file")
		flags.StringVar(&opts.SessionFile, "session-file", "", "External private Telegram session file")
		flags.StringVar(&opts.ConfigFile, "config-file", "", "Explicit private dotenv file; required for Telegram")
	case "restore":
		flags.StringVar(&archive, "archive", "", "Encrypted archive")
		flags.StringVar(&destination, "destination", "", "New, nonexistent data directory")
	case "verify":
		flags.StringVar(&archive, "archive", "", "Encrypted archive")
	default:
		return errors.New("usage: harbor-recovery backup|restore|verify [options]")
	}
	if err := flags.Parse(os.Args[2:]); err != nil {
		return errors.New("invalid recovery options; use --help")
	}
	if flags.NArg() != 0 || command == "backup" && (opts.DataDir == "" || opts.Output == "") || command != "backup" && archive == "" || command == "restore" && destination == "" {
		return errors.New("required recovery paths missing; use --help")
	}
	password, err := readPassword(passwordFile, command == "backup")
	if err != nil {
		return err
	}
	defer clear(password)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch command {
	case "backup":
		opts.Password = password
		err = recovery.Backup(ctx, opts)
	case "restore":
		err = recovery.Restore(ctx, archive, destination, password)
	case "verify":
		err = recovery.Verify(ctx, archive, password)
	}
	if err == nil {
		fmt.Println(recovery.Status(command))
	}
	return err
}

func readPassword(path string, confirm bool) ([]byte, error) {
	if path != "" {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 16 || info.Size() > 4098 {
			return nil, errors.New("backup password file must be private (0600), regular, and contain 16 to 4096 bytes")
		}
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, errors.New("cannot read private backup password file")
		}
		defer f.Close()
		data := make([]byte, info.Size())
		if _, err := io.ReadFull(f, data); err != nil {
			clear(data)
			return nil, errors.New("cannot read private backup password file")
		}
		if data[len(data)-1] == '\n' {
			data = data[:len(data)-1]
		}
		if len(data) > 0 && data[len(data)-1] == '\r' {
			data = data[:len(data)-1]
		}
		return validatePassword(data)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stderr.Fd())) {
		return nil, errors.New("provide --password-file or use an interactive terminal; no password is accepted in arguments or environment")
	}
	fmt.Fprint(os.Stderr, "Backup password (hidden, at least 16 bytes): ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, errors.New("backup password input interrupted")
	}
	if confirm {
		fmt.Fprint(os.Stderr, "Confirm backup password: ")
		repeated, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		match := err == nil && string(password) == string(repeated)
		clear(repeated)
		if !match {
			clear(password)
			return nil, errors.New("backup passwords did not match")
		}
	}
	return validatePassword(password)
}

func validatePassword(password []byte) ([]byte, error) {
	if len(password) < 16 || len(password) > 4096 {
		clear(password)
		return nil, errors.New("backup password must contain 16 to 4096 bytes")
	}
	return password, nil
}
