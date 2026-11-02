package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/viniciusdev772/br_panel/internal/nginx"
	"github.com/viniciusdev772/br_panel/internal/store"
)

// Defaults of the maintenance page when the admin leaves the texts empty.
const (
	defaultMaintTitle = "Em manutenção"
	defaultMaintText  = "Voltamos em instantes."
)

// siteApplyTimeout bounds a certificate round (DNS wait and certbot, one domain after another).
const siteApplyTimeout = 20 * time.Minute

// applyWeb copies the saved Nginx options of an app into the vhost description.
func applyWeb(site *nginx.Site, w store.Web) {
	if w.MaxBodyMB > 0 {
		site.MaxBody = fmt.Sprintf("%dm", w.MaxBodyMB)
	}
	site.ReadTimeout = w.ReadTimeout
	for _, x := range w.Domains {
		site.Extras = append(site.Extras, nginx.Extra{Domain: x.Name, To: x.To, Permanent: x.Permanent})
	}
	for _, u := range w.Users {
		site.Users = append(site.Users, u.Name+":"+u.Hash)
	}
	if w.Maintenance {
		m := &nginx.Maintenance{Title: w.MaintTitle, Text: w.MaintText}
		if m.Title == "" {
			m.Title = defaultMaintTitle
		}
		if m.Text == "" {
			m.Text = defaultMaintText
		}
		site.Maintenance = m
	}
}

// publishSite points the DNS of every domain of the app here (when Cloudflare is set up), then
// publishes the vhost and asks for the certificates that are missing.
func (d *Deployer) publishSite(ctx context.Context, log io.Writer, a store.App) error {
	d.ensureDNS(ctx, log, a.Domain)
	for _, x := range a.Web.Domains {
		d.ensureDNS(ctx, log, x.Name)
	}
	return d.nginx().Ensure(ctx, log, siteFor(a))
}

// ApplySite puts the saved site options of the app into nginx without a deploy. It reports whether
// anything was applied: an app without a domain, or never published, picks them up on its next
// deploy.
//
// Without withCerts only the vhost is rewritten and nginx reloaded, which takes a moment. With it,
// new domains also get their certificates; that waits for DNS and can take minutes, so it runs in
// the background and writes to the app's log.
func (d *Deployer) ApplySite(a store.App, withCerts bool) (bool, error) {
	if a.Domain == "" || !d.nginx().Published(a.Name) {
		return false, nil
	}
	if !withCerts {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return true, d.nginx().Reload(ctx, io.Discard, siteFor(a))
	}
	go func() {
		log := io.Writer(io.Discard)
		if f, err := os.OpenFile(d.Paths.LogFile(a.Name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640); err == nil {
			defer f.Close()
			log = f
		}
		ctx, cancel := context.WithTimeout(context.Background(), siteApplyTimeout)
		defer cancel()
		fmt.Fprintf(log, "\n== domínios e acesso: aplicando em %s ==\n", time.Now().Format(time.RFC3339))
		if err := d.publishSite(ctx, log, a); err != nil {
			fmt.Fprintf(log, "FALHOU: %v\n", err)
			return
		}
		fmt.Fprintln(log, "concluído")
	}()
	return true, nil
}
