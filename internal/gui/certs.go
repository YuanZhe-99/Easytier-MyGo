package gui

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/certmanager"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// Certificates (doc/en-us/gui.md, "Settings"; doc/en-us/certificates.md):
// the daemon's private CA, whether this computer trusts it, and the
// certificates it keeps current. The GUI never runs elevated, so trusting
// the CA is a command for the user to run.

// certsRefresh is how old the shown certificate state may be before the
// page reads it again (renewals happen in the daemon meanwhile).
const certsRefresh = 30 * time.Second

// certsState is the Certificates page's view state.
type certsState struct {
	loaded   bool
	loadedAt time.Time
	status   *certmanager.Status
	// err is why the state could not be read (an older daemon, a broken
	// certificate store).
	err string
}

// certJobDialog is Add or Edit certificate. job keeps the settings the
// dialog does not show (file names, modes, key type), so editing never
// drops them.
type certJobDialog struct {
	job     certmanager.Job
	editing bool
	network string // the Select's label: a network's name or None
	dns     string
	ips     string
	days    string // the Select's label
	hook    string
	err     string
}

// caDialog creates, imports or replaces the certificate authority.
type caDialog struct {
	tab     int // 0 create, 1 import
	replace bool
	name    string
	key     string // the Select's label
	years   string // the Select's label
	certPEM string
	keyPEM  string
	err     string
}

// dialogFootH is the button row under a scrolling dialog's body.
const dialogFootH float32 = 64

// certDays are the leaf lifetimes the dialog offers (397: the longest
// browsers accept).
var certDays = []int{30, 90, 180, 397}

// caYears are the CA lifetimes the dialog offers.
var caYears = []int{5, 10, 20}

// caKeyTypes pairs the dialog's key labels with their types; the RSA
// names are not translated.
func caKeyTypes() ([]string, []certmanager.KeyType) {
	return []string{L.CertKeyECDSA(), "RSA 2048", "RSA 4096"},
		[]certmanager.KeyType{certmanager.KeyECDSAP256, certmanager.KeyRSA2048, certmanager.KeyRSA4096}
}

// certsPage shows the CA, the trust store and the certificates.
// Side effects: reads the daemon's certificate state when it is missing
// or older than certsRefresh.
func (a *App) certsPage(c *ui.Context) {
	t, k := c.Theme(), tok(c)
	r := a.settingsRows(c)
	a.mu.Lock()
	cs, connected, busy := a.certsView, a.connected, a.busy["certs"]
	nets := slices.Clone(a.networks)
	a.mu.Unlock()
	if connected && !busy && (!cs.loaded || time.Since(cs.loadedAt) > certsRefresh) {
		a.loadCerts()
	}
	if cs.loaded {
		c.After(certsRefresh)
	}
	pageColumn(c, "set-certs", pageColumnW, func() {
		pageIntro(c, L.SettingsCerts(), L.SettingsCertsAbout())
		st := cs.status
		switch {
		case !connected:
			ui.Box(c).Height(14)
			callout(c, "certs-offline", k.warn, iconAlert, msgNotConnected(), nil)
			return
		case cs.err != "":
			ui.Box(c).Height(14)
			callout(c, "certs-err", k.warn, iconAlert, cs.err, nil)
			return
		case st == nil:
			ui.Row(c.Key("certs-loading")).Gap(8).AlignItems(ui.Center).Margin(18, 4, 0, 4).Children(func() {
				ui.Spinner(c)
				ui.Text(c, L.CertsLoading()).TextColor(t.TextMuted)
			})
			return
		}
		if st.Restricted {
			ui.Box(c).Height(14)
			callout(c, "certs-restricted", t.Accent, iconInfo, L.CertsRestricted(), nil)
		}
		a.caSection(c, r, st, busy)
		r.section(c, "cert-jobs", L.SectionCerts(), func() {
			if ui.Button(c.Key("cert-add"), L.ActionAddCert()).Disabled(busy || st.CA == nil).Clicked() {
				a.openCertJob(nil)
			}
		}, func(r *rows) {
			if len(st.Jobs) == 0 {
				about := L.CertNoneAbout()
				if st.CA == nil {
					about = L.CertNeedsCA()
				}
				r.row(c, "cert-none", iconLock, L.CertNone(), about, nil)
			}
			for _, j := range st.Jobs {
				a.certJobRow(c, r, j, nets, busy || st.CA == nil)
			}
		})
	})
}

// caSection is the certificate authority: create or import one, or the
// one in use with Replace, Export and whether this computer trusts it.
func (a *App) caSection(c *ui.Context, r *rows, st *certmanager.Status, busy bool) {
	t, k := c.Theme(), tok(c)
	r.section(c, "cert-ca", L.SectionCertAuthority(), nil, func(r *rows) {
		if st.CA == nil {
			r.row(c, "ca-none", iconShieldCheck, L.CertCANone(), L.CertCANoneAbout(), func() {
				if ui.Button(c.Key("ca-import"), L.ActionImportCA()).Disabled(busy).Clicked() {
					a.openCA(1, false)
				}
				if ui.PrimaryButton(c.Key("ca-create"), L.ActionCreateCA()).Disabled(busy).Clicked() {
					a.openCA(0, false)
				}
			})
			return
		}
		ca := st.CA
		about, col := L.CertCAValid(ca.NotAfter.Local().Format("2006-01-02")), ui.Color{}
		if !ca.Usable {
			about, col = L.CertCAProblem(ca.Problem), k.bad
		}
		name := ca.Name
		if name == "" {
			name = L.SectionCertAuthority()
		}
		r.rowIn(c, "ca", iconShieldCheck, name, about, col, func() {
			if ui.Button(c.Key("ca-replace"), L.ActionReplaceCA()).Disabled(busy).Clicked() {
				a.openCA(0, true)
			}
		})
		r.row(c, "ca-export", iconUpload, L.CertExportTitle(), L.CertExportAbout(), func() {
			if ui.Button(c.Key("ca-export-cert"), L.ActionExportCA()).Disabled(busy || !ca.Usable).Clicked() {
				a.exportCA(false)
			}
			if ui.Button(c.Key("ca-export-key"), L.ActionExportCAKey()).Disabled(busy || !ca.Usable).Clicked() {
				a.exportCA(true)
			}
		})
		tr := st.Trust
		switch {
		case tr.Installed:
			r.row(c, "ca-trust", iconCheck, L.CertTrusted(), L.CertTrustedAbout(), nil)
		case !tr.Supported || tr.InstallCommand == "":
			r.row(c, "ca-trust", iconShield, L.CertNotTrusted(), L.CertTrustUnsupported(), nil)
		default:
			r.rowIn(c, "ca-trust", iconShield, L.CertNotTrusted(), L.CertNotTrustedAbout(), k.warn, func() {
				if ui.Button(c.Key("ca-trust-copy"), L.ActionCopyCommand()).Clicked() {
					c.WriteClipboard(tr.InstallCommand)
					a.update(func() { a.setToast(L.ToastCommandCopied(), false) })
				}
			})
			r.block(c, "ca-trust-cmd", func() {
				ui.Text(c, tr.InstallCommand).Font("monospace").FontSize(sizeSmall).Selectable().
					Padding(8, 10).Radius(8).Background(tileBackground(t)).FillWidth()
			})
		}
	})
}

// certJobRow is one certificate: its name, its state in words, Renew now
// and Edit, then its names, folder and a failed command's output.
func (a *App) certJobRow(c *ui.Context, r *rows, j certmanager.JobStatus, nets []localapi.NetworkStatus, off bool) {
	t, k := c.Theme(), tok(c)
	id := j.Job.ID
	about, col := certPhase(j, nets), ui.Color{}
	if j.Phase == certmanager.PhaseFailed {
		col = k.bad
	}
	r.rowIn(c, "job-"+id, iconLock, certTitle(j, nets), about, col, func() {
		if j.HookWarning {
			pill(c, L.CertHookPill(), k.warn)
		}
		if ui.Button(c.Key("job-renew-"+id), L.ActionRenewCert()).Disabled(off).Clicked() {
			a.certDo(localapi.Request{Type: localapi.RequestCertRenew, Cert: &localapi.CertRequest{JobID: id, Force: true}},
				L.ToastCertRenewed(), nil, nil)
		}
		if ui.Button(c.Key("job-edit-"+id), L.ActionEditCert()).Disabled(off).Clicked() {
			job := j.Job
			a.openCertJob(&job)
		}
	})
	names := j.Names.All()
	if j.Cert != nil {
		names = j.Cert.SANs.All()
	}
	r.block(c, "job-"+id+"-more", func() {
		muted := func(s string) { ui.Text(c, s).FontSize(sizeSmall).TextColor(t.TextMuted).Selectable() }
		if len(names) > 0 {
			muted(strings.Join(names, ", "))
		}
		muted(L.CertSavedIn(j.Dir))
		if h := j.Hook; h != nil && !h.OK {
			ui.Text(c, L.CertHookFailed(h.Error)).FontSize(sizeSmall).TextColor(k.warn)
			if h.PermissionDenied {
				ui.Text(c, L.CertHookRights()).FontSize(sizeSmall).TextColor(k.warn)
			}
			if out := lastLines(h.Output, 4); out != "" {
				ui.Text(c, out).Font("monospace").FontSize(sizeSmall).TextColor(t.TextMuted).Selectable().
					Padding(6, 8).Radius(6).Background(tileBackground(t)).FillWidth()
			}
		}
	})
}

// certTitle names a certificate in the list: its name, else its first
// name or address, else its network's name (never an ID).
func certTitle(j certmanager.JobStatus, nets []localapi.NetworkStatus) string {
	if j.Job.Name != "" {
		return j.Job.Name
	}
	if j.Cert != nil && len(j.Cert.SANs.All()) > 0 {
		return j.Cert.SANs.All()[0]
	}
	if all := j.Names.All(); len(all) > 0 {
		return all[0]
	}
	if i := slices.IndexFunc(nets, func(n localapi.NetworkStatus) bool { return n.ID == j.Job.Network }); i >= 0 {
		return nets[i].Name
	}
	return L.CertUnnamed()
}

// certPhase is a certificate's state in words.
func certPhase(j certmanager.JobStatus, nets []localapi.NetworkStatus) string {
	switch j.Phase {
	case certmanager.PhaseFailed:
		return L.CertFailed(j.LastError)
	case certmanager.PhaseWaiting:
		name := L.CertUnnamed()
		if i := slices.IndexFunc(nets, func(n localapi.NetworkStatus) bool { return n.ID == j.Job.Network }); i >= 0 {
			name = nets[i].Name
		}
		return L.CertWaiting(name)
	case certmanager.PhaseOK:
		if j.Cert != nil {
			return L.CertValid(j.Cert.NotAfter.Local().Format("2006-01-02"))
		}
	}
	return L.CertPending()
}

// lastLines keeps the last n non-empty lines of a command's output.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// loadCerts reads the daemon's certificate state.
// Notes: marks the state loaded at once (under the lock, not through
// update) so the page asks only once per refresh.
func (a *App) loadCerts() {
	cl := a.client()
	if cl == nil {
		return
	}
	a.mu.Lock()
	a.busy["certs"] = true
	a.certsView.loaded, a.certsView.loadedAt = true, time.Now()
	a.mu.Unlock()
	go func() {
		resp, err := cl.Do(a.ctx, localapi.Request{Type: localapi.RequestCertStatus})
		a.update(func() {
			delete(a.busy, "certs")
			a.certsView.loadedAt = time.Now()
			if err != nil {
				a.certsView.status, a.certsView.err = nil, certsError(err)
				return
			}
			a.certsView.status, a.certsView.err = resp.Cert, ""
		})
	}()
}

// certsError words a failed certificate request; a daemon from before
// certificates answers "unknown request".
func certsError(err error) string {
	if localapi.CodeOf(err) == "" && strings.Contains(err.Error(), "unknown request") {
		return L.CertsOldService()
	}
	return L.CertsUnavailable(describeError(err))
}

// certDo runs a certificate request and shows the status it answers with.
// Inputs: ok is the toast on success (""), fail receives the error in
// words instead of a toast (nil: toast), then runs after success; both
// under a.mu.
func (a *App) certDo(req localapi.Request, ok string, fail func(msg string), then func()) {
	cl := a.client()
	if cl == nil {
		a.showToast(msgNotConnected(), true)
		return
	}
	a.update(func() { a.busy["certs"] = true })
	go func() {
		resp, err := cl.Do(a.ctx, req)
		a.update(func() {
			delete(a.busy, "certs")
			if err != nil {
				msg := describeError(err)
				if fail != nil {
					fail(msg)
				} else {
					a.setToast(msg, true)
				}
				return
			}
			if resp.Cert != nil {
				a.certsView.status, a.certsView.err, a.certsView.loadedAt = resp.Cert, "", time.Now()
			}
			if ok != "" {
				a.setToast(ok, false)
			}
			if then != nil {
				then()
			}
		})
	}()
}

// openCertJob opens Add certificate (job nil) or Edit certificate.
func (a *App) openCertJob(job *certmanager.Job) {
	a.update(func() {
		d := &certJobDialog{network: L.CertNetworkNone(), days: L.CertDays(strconv.Itoa(certmanager.DefaultLeafDays))}
		if job == nil {
			// The ID is fixed now: an administrator's approval is bound to it.
			d.job.ID = certmanager.NewJobID()
			// A new certificate starts with the network on screen.
			if n, ok := a.networkByID(a.selected); ok {
				d.job.Network, d.network = n.ID, n.Name
			}
		} else {
			d.job, d.editing = *job, true
			if n, ok := a.networkByID(job.Network); ok {
				d.network = n.Name
			}
			d.dns, d.ips = strings.Join(job.DNSNames, ", "), strings.Join(job.IPs, ", ")
			if job.ValidityDays != 0 {
				d.days = L.CertDays(strconv.Itoa(job.ValidityDays))
			}
			d.hook = job.Hook.Command
		}
		a.certDlg = d
	})
}

// certJobDialogView is Add or Edit certificate: a dialog over the window
// (and the Settings sheet), its fields as rows. In the privileged helper
// the folder and the command are not offered.
func (a *App) certJobDialogView(c *ui.Context) {
	a.mu.Lock()
	d := a.certDlg
	busy := a.busy["certs"]
	nets := slices.Clone(a.networks)
	restricted := a.certsView.status != nil && a.certsView.status.Restricted
	a.mu.Unlock()
	if d == nil {
		return
	}
	k := tok(c)
	title := L.CertJobAddTitle()
	if d.editing {
		title = L.CertJobEditTitle()
	}
	netOptions := []string{L.CertNetworkNone()}
	for _, n := range nets {
		netOptions = append(netOptions, n.Name)
	}
	var dayOptions []string
	for _, n := range certDays {
		dayOptions = append(dayOptions, L.CertDays(strconv.Itoa(n)))
	}
	if !slices.Contains(dayOptions, d.days) {
		dayOptions = append(dayOptions, d.days) // a lifetime set elsewhere
	}
	field := func(key string, v *string, placeholder string) ui.Element {
		return ui.TextInput(c.Key(key), v).Placeholder(placeholder).Width(rowControlW)
	}
	a.dialog(c, "cert-job", title, func() { a.update(func() { a.certDlg = nil }) }, func(r *rows) {
		ui.Text(c, L.CertJobAbout()).TextColor(c.Theme().TextMuted).Margin(12, 0, 14, 0)
		r.list(c, "cj-fields", func(r *rows) {
			r.row(c, "cj-name", nil, L.FieldDisplayName(), L.CertNameAbout(), func() {
				field("cj-name-in", &d.job.Name, "").AutoFocus()
			})
			r.row(c, "cj-net", nil, L.FieldCertNetwork(), L.CertNetworkAbout(), func() {
				ui.Select(c.Key("cj-net-sel"), &d.network, netOptions).Width(rowControlW).Label(L.FieldCertNetwork())
			})
			r.row(c, "cj-dns", nil, L.FieldCertDNS(), L.CertDNSAbout(), func() { field("cj-dns-in", &d.dns, "*.laptop.et.net") })
			r.row(c, "cj-ips", nil, L.FieldCertIPs(), L.CertIPsAbout(), func() { field("cj-ips-in", &d.ips, "192.168.1.20") })
			r.row(c, "cj-days", nil, L.FieldCertDays(), L.CertDaysAbout(), func() {
				ui.Select(c.Key("cj-days-sel"), &d.days, dayOptions).Width(rowControlW).Label(L.FieldCertDays())
			})
			// In the privileged helper both need an administrator's
			// approval when saved, and the command runs as administrator.
			folderAbout, hookAbout := L.CertFolderAbout(), L.CertHookAbout()
			if restricted {
				folderAbout, hookAbout = L.CertFolderHelper(), L.CertHookHelper()
			}
			r.row(c, "cj-dir", nil, L.FieldCertFolder(), folderAbout, func() {
				ui.TextInput(c.Key("cj-dir-in"), &d.job.OutDir).Width(rowControlW - 80)
				if ui.Button(c.Key("cj-dir-pick"), L.ActionChooseFolder()).Clicked() {
					a.chooseCertFolder(d)
				}
			})
			r.row(c, "cj-hook", nil, L.FieldCertHook(), hookAbout, func() {
				field("cj-hook-in", &d.hook, "caddy reload --config /etc/caddy/Caddyfile")
			})
		})
		if d.err != "" {
			ui.Text(c, d.err).FontSize(sizeSmall).TextColor(k.bad).Margin(10, 4, 0, 4)
		}
	}, func(close func()) {
		if d.editing && ui.Button(c.Key("cj-remove"), L.ActionRemove()).Disabled(busy).Clicked() {
			id := d.job.ID
			a.certDo(localapi.Request{Type: localapi.RequestCertJobRemove, Cert: &localapi.CertRequest{JobID: id}},
				L.ToastCertRemoved(), nil, func() { a.certDlg = nil })
		}
		ui.Spacer(c)
		if ui.Button(c.Key("cj-cancel"), L.ActionCancel()).Clicked() {
			close()
		}
		if ui.PrimaryButton(c.Key("cj-save"), L.ActionSave()).Disabled(busy).MinWidth(88).Clicked() {
			a.saveCertJob(d, nets)
		}
	})
}

// saveCertJob sends the dialog's job; the daemon checks it and issues the
// certificate at once. When the privileged helper wants an administrator's
// approval for the job's folder or command, the administrator prompt
// (`easytier-mygo-svc helper approve-cert`) comes first and the job is
// sent again; a cancelled prompt leaves the dialog open with the reason.
func (a *App) saveCertJob(d *certJobDialog, nets []localapi.NetworkStatus) {
	j, ok := jobFromDialog(d, nets)
	if !ok {
		a.update(func() { d.err = L.CertJobMissing() })
		return
	}
	cl := a.client()
	if cl == nil {
		a.showToast(msgNotConnected(), true)
		return
	}
	a.update(func() { a.busy["certs"], d.err = true, "" })
	go func() {
		req := localapi.Request{Type: localapi.RequestCertJobSet, Cert: &localapi.CertRequest{Job: &j}}
		resp, err := cl.Do(a.ctx, req)
		msg := ""
		if localapi.CodeOf(err) == localapi.CodeNeedsAdmin && certmanager.NeedsApproval(j) {
			if perr := approveCertJob(j); perr != nil {
				msg = perr.Error()
			} else {
				resp, err = cl.Do(a.ctx, req)
			}
		}
		if msg == "" && err != nil {
			msg = describeError(err)
		}
		a.update(func() {
			delete(a.busy, "certs")
			if msg != "" {
				d.err = msg
				return
			}
			a.certsView.status, a.certsView.err, a.certsView.loadedAt = resp.Cert, "", time.Now()
			a.setToast(L.ToastCertSaved(), false)
			a.certDlg = nil
		})
	}()
}

// approveCertJob asks for administrator rights to approve a job's folder
// and command in the privileged helper (pkexec, the macOS password, UAC).
// The job travels as base64url JSON, so no file is needed.
func approveCertJob(j certmanager.Job) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return runPrivileged("approve-cert", "-job", base64.RawURLEncoding.EncodeToString(data))
}

// jobFromDialog is the job the dialog describes: the edited job with the
// fields the dialog shows replaced, everything else (file names, modes,
// key type, a command given as arguments) kept.
// Returns: ok=false when it names no network, DNS name or address.
func jobFromDialog(d *certJobDialog, nets []localapi.NetworkStatus) (certmanager.Job, bool) {
	j := d.job
	j.Network = ""
	if i := slices.IndexFunc(nets, func(n localapi.NetworkStatus) bool { return n.Name == d.network }); i >= 0 {
		j.Network = nets[i].ID
	}
	j.DNSNames, j.IPs = splitNames(d.dns), splitNames(d.ips)
	j.Name, j.OutDir = strings.TrimSpace(j.Name), strings.TrimSpace(j.OutDir)
	// A lifetime set elsewhere (the CLI) stays unless another is chosen.
	for _, n := range certDays {
		if d.days == L.CertDays(strconv.Itoa(n)) {
			j.ValidityDays = n
		}
	}
	switch hook := strings.TrimSpace(d.hook); {
	case hook != "":
		j.Hook.Enabled, j.Hook.Command, j.Hook.Args = true, hook, nil
	case len(j.Hook.Args) == 0:
		j.Hook = certmanager.Hook{} // a command set as arguments (CLI) is kept
	}
	return j, j.Network != "" || len(j.DNSNames) > 0 || len(j.IPs) > 0
}

// splitNames splits a comma- or space-separated list.
func splitNames(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' })
}

// chooseCertFolder lets the user pick the output folder.
func (a *App) chooseCertFolder(d *certJobDialog) {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, Title: L.FieldCertFolder(), Directory: true, CreateDirectories: true})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() {
			if a.certDlg == d {
				d.job.OutDir = paths[0]
			}
		})
	}()
}

// openCA opens the certificate authority dialog at Create (tab 0) or
// Import (1); replace when one exists.
func (a *App) openCA(tab int, replace bool) {
	keys, _ := caKeyTypes()
	a.update(func() {
		a.caDlg = &caDialog{tab: tab, replace: replace, key: keys[0], years: L.CertYears(strconv.Itoa(certmanager.DefaultCAYears))}
	})
}

// caDialogView creates or imports the certificate authority; replacing
// one says what it means first.
func (a *App) caDialogView(c *ui.Context) {
	a.mu.Lock()
	d := a.caDlg
	busy := a.busy["certs"]
	a.mu.Unlock()
	if d == nil {
		return
	}
	t, k := c.Theme(), tok(c)
	keyLabels, keyTypes := caKeyTypes()
	var yearOptions []string
	for _, n := range caYears {
		yearOptions = append(yearOptions, L.CertYears(strconv.Itoa(n)))
	}
	a.dialog(c, "ca", L.SectionCertAuthority(), func() { a.update(func() { a.caDlg = nil }) }, func(r *rows) {
		ui.Row(c.Key("ca-tabs")).Margin(14, 0, 0, 0).Children(func() {
			ui.Segmented(c.Key("ca-tab"), &d.tab, L.CertCAModeCreate(), L.CertCAModeImport())
		})
		if d.replace {
			ui.Box(c).Height(12)
			callout(c, "ca-replace-warn", k.warn, iconAlert, L.CertReplaceWarn(), nil)
		}
		if d.tab == 0 {
			r.section(c, "ca-new", L.CertCAModeCreate(), nil, func(r *rows) {
				r.row(c, "ca-name", nil, L.FieldDisplayName(), L.CertCANameAbout(), func() {
					ui.TextInput(c.Key("ca-name-in"), &d.name).Placeholder(certmanager.DefaultCACommonName).Width(rowControlW)
				})
				r.row(c, "ca-key", nil, L.FieldCAKeyType(), L.CertKeyAbout(), func() {
					ui.Select(c.Key("ca-key-sel"), &d.key, keyLabels).Width(rowControlW).Label(L.FieldCAKeyType())
				})
				r.row(c, "ca-years", nil, L.FieldCertDays(), L.CertYearsAbout(), func() {
					ui.Select(c.Key("ca-years-sel"), &d.years, yearOptions).Width(rowControlW).Label(L.FieldCertDays())
				})
			})
		} else {
			r.section(c, "ca-in", L.CertCAModeImport(), nil, func(r *rows) {
				pem := func(key, title, about string, v *string, placeholder string) {
					r.row(c, key, nil, title, about, func() {
						if ui.Button(c.Key(key+"-pick"), L.ActionChooseFile()).Clicked() {
							a.readPEMInto(d, v, title)
						}
					})
					r.block(c, key+"-text", func() {
						ui.TextArea(c.Key(key+"-in"), v).Placeholder(placeholder).FillWidth().MinHeight(72).Font("monospace").FontSize(sizeSmall)
					})
				}
				pem("ca-cert", L.FieldCACert(), L.CertCACertAbout(), &d.certPEM, "-----BEGIN CERTIFICATE-----")
				pem("ca-keyfile", L.FieldCAKey(), L.CertCAKeyAbout(), &d.keyPEM, "-----BEGIN PRIVATE KEY-----")
			})
		}
		ui.Text(c, L.CertCAAbout()).FontSize(sizeSmall).TextColor(t.TextMuted).Margin(10, 4, 0, 4)
		if d.err != "" {
			ui.Text(c, d.err).FontSize(sizeSmall).TextColor(k.bad).Margin(8, 4, 0, 4)
		}
	}, func(close func()) {
		ui.Spacer(c)
		if ui.Button(c.Key("ca-cancel"), L.ActionCancel()).Clicked() {
			close()
		}
		label := L.ActionCreateCA()
		switch {
		case d.replace:
			label = L.ActionReplace()
		case d.tab == 1:
			label = L.CertCAModeImport()
		}
		if ui.PrimaryButton(c.Key("ca-go"), label).Disabled(busy).MinWidth(88).Clicked() {
			req := localapi.CertRequest{Replace: d.replace}
			if d.tab == 0 {
				opts := certmanager.CAOptions{CommonName: strings.TrimSpace(d.name)}
				if i := slices.Index(keyLabels, d.key); i >= 0 {
					opts.KeyType = keyTypes[i]
				}
				for _, n := range caYears {
					if d.years == L.CertYears(strconv.Itoa(n)) {
						opts.Years = n
					}
				}
				req.CA = &opts
				a.certDo(localapi.Request{Type: localapi.RequestCertCAGenerate, Cert: &req}, L.ToastCAReady(),
					func(msg string) { d.err = msg }, func() { a.caDlg = nil })
			} else {
				if strings.TrimSpace(d.certPEM) == "" || strings.TrimSpace(d.keyPEM) == "" {
					a.update(func() { d.err = L.CertImportMissing() })
					return
				}
				req.CertPEM, req.KeyPEM = d.certPEM, d.keyPEM
				a.certDo(localapi.Request{Type: localapi.RequestCertCAImport, Cert: &req}, L.ToastCAReady(),
					func(msg string) { d.err = msg }, func() { a.caDlg = nil })
			}
		}
	})
}

// readPEMInto fills a PEM field from a file the user picks.
func (a *App) readPEMInto(d *caDialog, v *string, title string) {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, Title: title,
			Filters: []mygo.FileFilter{{Name: "PEM", Extensions: []string{"pem", "crt", "cer", "key"}}}})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := os.ReadFile(paths[0])
		a.update(func() {
			if a.caDlg != d {
				return
			}
			if err != nil {
				d.err = err.Error()
				return
			}
			*v, d.err = string(data), ""
		})
	}()
}

// exportCA saves the CA certificate, or with withKey (after a warning) the
// certificate and its private key in one PEM file (mode 0600) that Import
// accepts for both fields.
func (a *App) exportCA(withKey bool) {
	cl := a.client()
	if cl == nil {
		a.showToast(msgNotConnected(), true)
		return
	}
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		if withKey {
			res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: w, Type: mygo.MessageWarning,
				Message: L.CertExportKeyTitle(), Detail: L.CertExportKeyBody(),
				Buttons: []string{L.CertExportTitle(), L.ActionCancel()}, DefaultButton: 1, CancelButton: 1})
			if err != nil || res.Button != 0 {
				return
			}
		}
		resp, err := cl.Do(a.ctx, localapi.Request{Type: localapi.RequestCertCAExport, Cert: &localapi.CertRequest{WithKey: withKey}})
		if err != nil {
			a.showToast(describeError(err), true)
			return
		}
		name, data, mode := "easytier-mygo-ca.crt", resp.CertExport.CertPEM, os.FileMode(0o644)
		if withKey {
			name, data, mode = "easytier-mygo-ca-with-key.pem", resp.CertExport.CertPEM+resp.CertExport.KeyPEM, 0o600
		}
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w, Title: L.CertExportTitle(), DefaultPath: name})
		if err != nil || path == "" {
			return
		}
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			a.showToast(err.Error(), true)
			return
		}
		_ = os.Chmod(path, mode) // an existing file keeps its mode otherwise
		a.showToast(L.ToastCAExported(), false)
	}()
}

// dialog is a dialog over the dimmed window like Add network: a title
// with ×, the body's rows, then the foot's buttons. onClose runs when it
// is closed by ×, Escape, a click outside or the foot's close.
func (a *App) dialog(c *ui.Context, key, title string, onClose func(), body func(r *rows), foot func(close func())) {
	t := c.Theme()
	_, winH := c.Size()
	open := true
	closeFn := func() { open = false }
	ui.DialogBase(c, &open, func(back, panel ui.Element) {
		back.Background(sheetScrim).Padding(dialogTop, sheetX, sheetBottom, sheetX).AlignItems(ui.Center).Justify(ui.Start)
		panel.Radius(paneRadius).Background(t.Background).Shadow(0, 10, 30, 0, sheetShadow).Clip()
		ui.Column(c.Key(key)).Width(dialogW + 80).Children(func() {
			ui.Row(c.Key(key+"-head")).Height(sheetHeaderH).Shrink(0).Padding(0, 12, 0, 20).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(sizeHeading).Bold().Grow(1)
				if iconButton(c, key+"-close", iconClose, L.ActionClose(), false) {
					open = false
				}
			})
			ui.Divider(c)
			// The body scrolls; the buttons stay in view under it.
			ui.Scroll(c.Key(key + "-scroll")).FillWidth().MaxHeight(max(winH-dialogTop-sheetBottom-sheetHeaderH-dialogFootH, 200)).Children(func() {
				ui.Column(c.Key(key+"-body")).FillWidth().Padding(4, 20, 6, 20).Children(func() {
					var r rows
					body(&r)
				})
			})
			ui.Row(c.Key(key+"-foot")).FillWidth().Height(dialogFootH).Shrink(0).Padding(0, 20).Gap(8).AlignItems(ui.Center).Children(func() {
				foot(closeFn)
			})
		})
	})
	if ownWindowButtons {
		ui.Overlay(c, func() {
			ui.Row(c.Key(key + "-win")).Absolute().Top(paneMargin + (paneHeaderH-winButtonH)/2).Right(2 * paneMargin).Gap(8).Children(func() {
				a.windowButtons(c, key+"-win", true)
			})
		})
	}
	if !open {
		onClose()
	}
}
