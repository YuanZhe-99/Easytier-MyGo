package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/certmanager"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/helper"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

const certUsage = `usage: easytier-mygo-ctl cert COMMAND
  status [-json]                          CA, trust store and every job
  ca-generate [-cn NAME] [-org ORG] [-years N] [-key ecdsa-p256|rsa-2048|rsa-4096] [-replace]
  ca-import [-replace] CERT.pem KEY.pem   use an existing CA (PEM files)
  ca-export [-key KEY.pem] CA.crt         write the CA certificate (and, with -key, its private key)
  job-set [-file JOB.json | flags]        add or change a job, then check it
      -id ID -name NAME -network NETWORK-ID -dns a.et.net,*.et.net -ip 10.0.0.1
      -out DIR -cert-file NAME -fullchain-file NAME -key-file NAME -cert-mode 0644 -key-mode 0600
      -days 90 -renew-before 30 -key-type ecdsa-p256 -hook 'COMMAND' -shell sh|cmd|powershell -hook-timeout 30
  job-remove ID                           forget a job (its files stay)
  renew [-force] [ID]                     check now; -force issues new certificates
  trust status|install|uninstall [-file CA.crt]
                                          the system trust store, with this process's rights`

// runCert handles `easytier-mygo-ctl cert ...`.
func runCert(ctx context.Context, c *localapi.Client, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, certUsage)
		os.Exit(2)
	}
	do := func(t localapi.RequestType, r localapi.CertRequest) (*localapi.Response, error) {
		return c.Do(ctx, localapi.Request{Type: t, Cert: &r})
	}
	fs := flag.NewFlagSet("cert "+args[0], flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, certUsage) }
	switch args[0] {
	case "status":
		asJSON := fs.Bool("json", false, "print JSON")
		fs.Parse(args[1:])
		r, err := do(localapi.RequestCertStatus, localapi.CertRequest{})
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(r.Cert)
		}
		printCertStatus(r.Cert)
	case "ca-generate":
		cn := fs.String("cn", "", "common name (default "+certmanager.DefaultCACommonName+")")
		org := fs.String("org", "", "organization")
		years := fs.Int("years", 0, "validity in years (default 10)")
		key := fs.String("key", "", "key type: ecdsa-p256 (default), rsa-2048, rsa-4096")
		replace := fs.Bool("replace", false, "replace the existing CA (every certificate is re-issued)")
		fs.Parse(args[1:])
		r, err := do(localapi.RequestCertCAGenerate, localapi.CertRequest{Replace: *replace,
			CA: &certmanager.CAOptions{CommonName: *cn, Organization: *org, Years: *years, KeyType: certmanager.KeyType(*key)}})
		if err != nil {
			return replaceHint(err)
		}
		printCertStatus(r.Cert)
	case "ca-import":
		replace := fs.Bool("replace", false, "replace the existing CA")
		fs.Parse(args[1:])
		if fs.NArg() != 2 {
			fs.Usage()
			os.Exit(2)
		}
		certPEM, err := os.ReadFile(fs.Arg(0))
		if err != nil {
			return err
		}
		keyPEM, err := os.ReadFile(fs.Arg(1))
		if err != nil {
			return err
		}
		r, err := do(localapi.RequestCertCAImport, localapi.CertRequest{CertPEM: string(certPEM), KeyPEM: string(keyPEM), Replace: *replace})
		if err != nil {
			return replaceHint(err)
		}
		printCertStatus(r.Cert)
	case "ca-export":
		keyFile := fs.String("key", "", "also write the CA's private key to this file (mode 0600)")
		fs.Parse(args[1:])
		if fs.NArg() != 1 {
			fs.Usage()
			os.Exit(2)
		}
		r, err := do(localapi.RequestCertCAExport, localapi.CertRequest{WithKey: *keyFile != ""})
		if err != nil {
			return err
		}
		if err := os.WriteFile(fs.Arg(0), []byte(r.CertExport.CertPEM), 0o644); err != nil {
			return err
		}
		fmt.Printf("CA certificate written to %s\nSHA-256 fingerprint: %s\n", fs.Arg(0), r.CertExport.Fingerprint)
		if *keyFile != "" {
			if err := os.WriteFile(*keyFile, []byte(r.CertExport.KeyPEM), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "CA private key written to %s.\n"+
				"Anyone with this key can issue certificates every device that trusts this CA accepts.\n"+
				"Copy it only to computers you control, over a private channel, and delete this copy afterwards.\n", *keyFile)
		}
	case "job-set":
		job, err := jobFromFlags(fs, args[1:])
		if err != nil {
			return err
		}
		if job.ID == "" {
			// An approval is bound to the ID, so the retry must use the same.
			job.ID = certmanager.NewJobID()
		}
		r, err := do(localapi.RequestCertJobSet, localapi.CertRequest{Job: &job})
		if localapi.CodeOf(err) == localapi.CodeNeedsAdmin && certmanager.NeedsApproval(job) {
			return approvalHint(job, err)
		}
		if err != nil {
			return err
		}
		printCertStatus(r.Cert)
	case "job-remove":
		fs.Parse(args[1:])
		if fs.NArg() != 1 {
			fs.Usage()
			os.Exit(2)
		}
		r, err := do(localapi.RequestCertJobRemove, localapi.CertRequest{JobID: fs.Arg(0)})
		if err != nil {
			return err
		}
		printCertStatus(r.Cert)
	case "renew":
		force := fs.Bool("force", false, "issue new certificates even when the current ones are fine")
		fs.Parse(args[1:])
		r, err := do(localapi.RequestCertRenew, localapi.CertRequest{JobID: fs.Arg(0), Force: *force})
		if err != nil {
			return err
		}
		printCertStatus(r.Cert)
	case "trust":
		return runTrust(ctx, c, fs, args[1:])
	default:
		fmt.Fprintln(os.Stderr, certUsage)
		os.Exit(2)
	}
	return nil
}

// approvalHint explains how an administrator approves a certificate's
// folder and command in the privileged helper: the job goes into a file,
// the administrator runs `easytier-mygo-svc helper approve-cert` on it,
// and the same file is saved again.
// Returns: the error to print (the job file's path is in it).
func approvalHint(job certmanager.Job, cause error) error {
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "easytier-mygo-cert-"+job.ID+"-*.json")
	if err != nil {
		return err
	}
	f.Write(append(data, '\n'))
	f.Close()
	svc := approverBinary()
	approve := "sudo " + svc + " helper approve-cert -file " + f.Name()
	where := "in a terminal"
	if runtime.GOOS == "windows" {
		approve = "& '" + svc + "' helper approve-cert -file '" + f.Name() + "'"
		where = "in an administrator PowerShell"
	}
	return fmt.Errorf("%v\n\nThe privileged helper runs as administrator, so an administrator approves this certificate's folder and command once. Run %s:\n  %s\nthen save it again from the same file:\n  easytier-mygo-ctl cert job-set -file %s\nThe command after renewal will run as administrator.",
		cause, where, approve, f.Name())
}

// approverBinary is the easytier-mygo-svc to approve with: the helper's
// installed (administrator-owned) copy, else the one next to this program.
func approverBinary() string {
	name := "easytier-mygo-svc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if helper.BinDir != "" {
		if p := filepath.Join(helper.BinDir, name); fileExists(p) {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), name); fileExists(p) {
			return p
		}
	}
	return name
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func replaceHint(err error) error {
	if localapi.CodeOf(err) == localapi.CodeConflict {
		return fmt.Errorf("%v\nadd -replace to replace it", err)
	}
	return err
}

// jobFromFlags reads a job from -file or from flags.
func jobFromFlags(fs *flag.FlagSet, args []string) (certmanager.Job, error) {
	var j certmanager.Job
	file := fs.String("file", "", "job as JSON (- for standard input)")
	fs.StringVar(&j.ID, "id", "", "job ID (generated when empty; an existing ID is replaced)")
	fs.StringVar(&j.Name, "name", "", "display name")
	fs.StringVar(&j.Network, "network", "", "network ID whose MagicDNS name, hostname and virtual IP are included")
	dns := fs.String("dns", "", "extra DNS names, comma-separated (wildcards allowed)")
	ips := fs.String("ip", "", "extra IP addresses, comma-separated")
	fs.StringVar(&j.OutDir, "out", "", "output directory (absolute; default: the daemon's state directory)")
	fs.StringVar(&j.CertFile, "cert-file", "", "certificate file name (- to skip)")
	fs.StringVar(&j.FullchainFile, "fullchain-file", "", "full chain file name (- to skip)")
	fs.StringVar(&j.KeyFile, "key-file", "", "key file name")
	fs.StringVar(&j.CertMode, "cert-mode", "", "certificate file mode (octal, default 0644)")
	fs.StringVar(&j.KeyMode, "key-mode", "", "key file mode (octal, default 0600)")
	fs.IntVar(&j.ValidityDays, "days", 0, "certificate validity in days (default 90, at most 397)")
	fs.IntVar(&j.RenewBeforeDays, "renew-before", 0, "renew when fewer days remain (default 30)")
	keyType := fs.String("key-type", "", "leaf key type")
	hook := fs.String("hook", "", "command to run after a new certificate was written")
	shell := fs.String("shell", "", "shell for -hook: sh, cmd or powershell")
	timeout := fs.Int("hook-timeout", 0, "hook timeout in seconds (default 30)")
	fs.Parse(args)
	if *file != "" {
		var data []byte
		var err error
		if *file == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(*file)
		}
		if err != nil {
			return j, err
		}
		var fromFile certmanager.Job
		if err := json.Unmarshal(data, &fromFile); err != nil {
			return j, fmt.Errorf("%s: %w", *file, err)
		}
		return fromFile, nil
	}
	j.DNSNames, j.IPs, j.KeyType = splitList(*dns), splitList(*ips), certmanager.KeyType(*keyType)
	if j.OutDir != "" {
		abs, err := filepath.Abs(j.OutDir)
		if err != nil {
			return j, err
		}
		j.OutDir = abs
	}
	if *hook != "" {
		j.Hook = certmanager.Hook{Enabled: true, Command: *hook, Shell: certmanager.Shell(*shell), TimeoutSeconds: *timeout}
	}
	return j, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runTrust installs, removes or inspects the CA in the system trust store
// from this process, with the rights it was started with. Without them it
// prints the command to run in an administrator shell.
func runTrust(ctx context.Context, c *localapi.Client, fs *flag.FlagSet, args []string) error {
	file := fs.String("file", "", "CA certificate file (default: ask the daemon)")
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	action := args[0]
	fs.Parse(args[1:])
	cert, path, cleanup, err := trustTarget(ctx, c, *file)
	if err != nil {
		return err
	}
	defer cleanup()
	switch action {
	case "status":
		st := certmanager.Trust(cert, path)
		fmt.Printf("CA:          %s\nfingerprint: %s\n", cert.Subject.CommonName, certmanager.Fingerprint(cert))
		if !st.Supported {
			fmt.Println("trust store: not supported on this system")
			return nil
		}
		fmt.Printf("trust store: %s\ninstalled:   %v\nelevated:    %v\n", st.Store, st.Installed, st.Privileged)
		if st.Error != "" {
			fmt.Println("check error:", st.Error)
		}
		if !st.Privileged {
			if st.Installed {
				fmt.Println("to remove it, run in an administrator shell:\n  " + st.UninstallCommand)
			} else {
				fmt.Println("to install it, run in an administrator shell:\n  " + st.InstallCommand)
			}
		}
		return nil
	case "install", "uninstall":
		install := certmanager.InstallTrust
		if action == "uninstall" {
			install = certmanager.UninstallTrust
		}
		err := install(cert, path)
		var pe *certmanager.PrivilegeError
		if errors.As(err, &pe) {
			return fmt.Errorf("not enough rights to change the system trust store from this process.\n"+
				"Run this in an administrator shell (sudo, or PowerShell \"Run as administrator\"):\n  %s", pe.Command)
		}
		if err != nil {
			return err
		}
		if action == "install" {
			fmt.Println("the CA is now trusted by this computer; browsers may need a restart (Firefox keeps its own store)")
		} else {
			fmt.Println("the CA is no longer trusted by this computer")
		}
		return nil
	}
	fs.Usage()
	os.Exit(2)
	return nil
}

// trustTarget finds the CA certificate: a file, or the daemon's CA. For
// the daemon's CA path is the daemon's own file (the manual commands name
// it) unless this process cannot read it; then a temporary copy is used.
func trustTarget(ctx context.Context, c *localapi.Client, file string) (*x509.Certificate, string, func(), error) {
	noop := func() {}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, "", noop, err
		}
		cert, err := certmanager.ParseCertificatePEM(data)
		if err != nil {
			return nil, "", noop, fmt.Errorf("%s: %w", file, err)
		}
		abs, _ := filepath.Abs(file)
		return cert, abs, noop, nil
	}
	st, err := c.Do(ctx, localapi.Request{Type: localapi.RequestCertStatus})
	if err != nil {
		return nil, "", noop, fmt.Errorf("%w\n(the daemon is needed to find the CA; or pass -file CA.crt, written by `cert ca-export`)", err)
	}
	exp, err := c.Do(ctx, localapi.Request{Type: localapi.RequestCertCAExport, Cert: &localapi.CertRequest{}})
	if err != nil {
		return nil, "", noop, err
	}
	cert, err := certmanager.ParseCertificatePEM([]byte(exp.CertExport.CertPEM))
	if err != nil {
		return nil, "", noop, err
	}
	path := st.Cert.CA.Path
	if _, err := os.ReadFile(path); err == nil {
		return cert, path, noop, nil
	}
	tmp, err := os.CreateTemp("", "easytier-mygo-ca-*.crt")
	if err != nil {
		return nil, "", noop, err
	}
	tmp.WriteString(exp.CertExport.CertPEM)
	tmp.Close()
	return cert, tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}

// printCertStatus prints the certificate state for a person.
func printCertStatus(s *certmanager.Status) {
	if s == nil {
		return
	}
	if s.CA == nil {
		fmt.Println("CA: none (create one with `cert ca-generate`, or `cert ca-import`)")
	} else {
		state := "ready"
		if !s.CA.Usable {
			state = "not usable: " + s.CA.Problem
		}
		fmt.Printf("CA: %s (%s)\n  fingerprint %s\n  valid until %s, %s\n  file %s\n", s.CA.Subject, s.CA.KeyType,
			s.CA.Fingerprint, s.CA.NotAfter.Local().Format(time.DateOnly), state, s.CA.Path)
		switch {
		case !s.Trust.Supported:
			fmt.Println("  system trust store: not supported here")
		case s.Trust.Installed:
			fmt.Printf("  trusted by this computer (%s)\n", s.Trust.Store)
		default:
			fmt.Printf("  not trusted by this computer yet: run `easytier-mygo-ctl cert trust install`, or in an administrator shell:\n    %s\n", s.Trust.InstallCommand)
		}
	}
	if s.Restricted {
		fmt.Println("privileged helper: a folder of your choice or a command after renewal needs an administrator's approval (job-set explains how); commands run as administrator")
	}
	for _, j := range s.Jobs {
		name := j.Job.ID
		if j.Job.Name != "" {
			name += " (" + j.Job.Name + ")"
		}
		fmt.Printf("job %s: %s\n", name, j.Phase)
		if j.Job.Network != "" {
			fmt.Printf("  network %s\n", j.Job.Network)
		}
		if j.Cert != nil {
			fmt.Printf("  names: %s\n  valid until %s\n", strings.Join(j.Cert.SANs.All(), ", "), j.Cert.NotAfter.Local().Format(time.DateTime))
		} else if !j.Names.Empty() {
			fmt.Printf("  names: %s\n", strings.Join(j.Names.All(), ", "))
		}
		fmt.Printf("  files: %s\n", strings.Join(nonEmpty(j.Files.Cert, j.Files.Fullchain, j.Files.Key), ", "))
		if !j.LastIssued.IsZero() {
			fmt.Printf("  last issued %s (%s)\n", j.LastIssued.Local().Format(time.DateTime), j.LastReason)
		}
		if j.Phase == certmanager.PhaseWaiting {
			fmt.Println("  waiting for the network to connect so its name and address are known (or run `cert renew -force` to issue without them)")
		}
		if j.LastError != "" {
			fmt.Println("  error:", j.LastError)
		}
		if h := j.Hook; h != nil {
			if h.OK {
				fmt.Printf("  hook ran %s in %d ms\n", h.Time.Local().Format(time.DateTime), h.DurationMS)
			} else {
				fmt.Printf("  hook FAILED %s: %s\n", h.Time.Local().Format(time.DateTime), h.Error)
				if h.PermissionDenied {
					fmt.Println("  (looks like missing rights: the daemon runs the hook as the user it runs as)")
				}
			}
			if out := strings.TrimSpace(h.Output); out != "" {
				fmt.Println("  output:\n    " + strings.ReplaceAll(out, "\n", "\n    "))
			}
		}
	}
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
