package deploy

import (
	"context"
	"testing"
	"time"
)

// Deleting an app never waits for its deploy: the deploy is cancelled, the queued re-run is dropped
// and the app is gone at once.
func TestRemoveCancelsRunningDeploy(t *testing.T) {
	d, st := mountDeployer(t)
	if _, ok := st.BeginDeploy("web"); !ok {
		t.Fatal("BeginDeploy")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := d.beginRun("web", cancel)
	finished := make(chan struct{})
	go func() { // the build: it only ends when cancelled
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond) // letting go of the files takes a moment
		d.endRun("web", r)
		close(finished)
	}()
	d.MarkPending("web") // a push arrived during the deploy

	start := time.Now()
	if err := d.Remove("web"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("excluir esperou o deploy: %v", time.Since(start))
	}
	select {
	case <-finished:
	default:
		t.Error("Remove devolveu antes de o deploy terminar de soltar os arquivos")
	}
	if _, ok := st.Get("web"); ok {
		t.Error("a aplicação deveria ter sido excluída")
	}
	if d.takePending("web") {
		t.Error("a repetição pendente deveria ter sido descartada")
	}
	if d.cancelRun("web") {
		t.Error("não sobra deploy registrado")
	}
}
