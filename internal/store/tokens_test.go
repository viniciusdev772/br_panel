package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestTokens(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id, err := st.CreateToken("notebook", "h1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateToken("notebook", "h2"); !errors.Is(err, ErrExists) {
		t.Errorf("nome repetido: %v", err)
	}
	if _, err := st.CreateToken("outro", "h1"); !errors.Is(err, ErrExists) {
		t.Errorf("hash repetido: %v", err)
	}
	tok, ok := st.TokenByHash("h1")
	if !ok || tok.ID != id || tok.Name != "notebook" || !tok.LastUsed.IsZero() {
		t.Fatalf("TokenByHash: %+v %v", tok, ok)
	}
	if _, ok := st.TokenByHash("nada"); ok {
		t.Error("hash desconhecido não pode achar token")
	}
	st.TouchToken(id)
	if got, _ := st.TokenByHash("h1"); got.LastUsed.IsZero() {
		t.Error("TouchToken não gravou o uso")
	}
	if l := st.ListTokens(); len(l) != 1 || l[0].Name != "notebook" {
		t.Errorf("ListTokens: %+v", l)
	}
	if err := st.DeleteToken(id); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteToken(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("revogar duas vezes: %v", err)
	}
	if _, ok := st.TokenByHash("h1"); ok {
		t.Error("token revogado ainda vale")
	}
}
