// Command sign manages the release key that lets the privileged helper
// update itself without a prompt (internal/bundlesig).
//
//	go run ./tools/sign keygen -out ~/easytier-mygo-release.key
//	    writes the private key (keep it secret: a GitHub secret, never the
//	    repository) and prints the public key for internal/bundlesig/release.pub
//	go run ./tools/sign sign -dir resources/linux-amd64/bin -version 0.1.0
//	    signs a bundle with the key in $EASYTIER_MYGO_SIGNING_KEY
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/bundlesig"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		fs := flag.NewFlagSet("keygen", flag.ExitOnError)
		out := fs.String("out", "", "where to write the private key")
		fs.Parse(os.Args[2:])
		err = keygen(*out)
	case "sign":
		fs := flag.NewFlagSet("sign", flag.ExitOnError)
		dir := fs.String("dir", "", "bundle bin directory")
		version := fs.String("version", "", "product version")
		fs.Parse(os.Args[2:])
		err = sign(*dir, *version)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./tools/sign keygen -out FILE | sign -dir DIR -version V")
	os.Exit(2)
}

func keygen(out string) error {
	if out == "" {
		return fmt.Errorf("-out is required")
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func sign(dir, version string) error {
	s := strings.TrimSpace(os.Getenv("EASYTIER_MYGO_SIGNING_KEY"))
	if s == "" {
		return fmt.Errorf("EASYTIER_MYGO_SIGNING_KEY is not set")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PrivateKeySize {
		return fmt.Errorf("EASYTIER_MYGO_SIGNING_KEY is malformed")
	}
	return bundlesig.Sign(dir, version, ed25519.PrivateKey(b))
}
