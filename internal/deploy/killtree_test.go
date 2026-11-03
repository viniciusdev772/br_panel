package deploy

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	return len(f) > 0 && f[0] != "Z" // a zombie is dead, only not reaped yet
}

// A build started through runuser lives in another session than the panel's child; killTree must
// still reach it and everything it spawned.
func TestKillTreeReachesOtherSessions(t *testing.T) {
	// sh (own session, like runuser's child) → sleep in the background → a grandchild shell.
	c := exec.Command("sh", "-c", "sleep 60 & sh -c 'sleep 60 & wait' & wait")
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }()
	time.Sleep(300 * time.Millisecond) // let the shells fork

	var before []int
	children := func(parent int) []int {
		out, _ := exec.Command("pgrep", "-P", strconv.Itoa(parent)).Output()
		var r []int
		for _, f := range strings.Fields(string(out)) {
			if n, err := strconv.Atoi(f); err == nil {
				r = append(r, n)
			}
		}
		return r
	}
	for _, p := range children(c.Process.Pid) {
		before = append(before, p)
		before = append(before, children(p)...)
	}
	if len(before) < 3 {
		t.Skipf("não consegui montar a árvore de processos (%v)", before)
	}
	killTree(c.Process.Pid)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("o processo raiz não morreu")
	}
	time.Sleep(200 * time.Millisecond)
	for _, p := range before {
		if alive(p) {
			t.Errorf("o descendente %d continua vivo", p)
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
	}
}
