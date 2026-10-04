package gemeinsam

import (
	"os"
	"path/filepath"
)

// SchreibeDatei schreibt atomar: erst in eine temporaere Datei, dann
// umbenennen. Faellt der Strom waehrend des Schreibens aus, bleibt die alte
// Datei erhalten statt einer leeren oder halben.
func SchreibeDatei(pfad string, inhalt []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(pfad), filepath.Base(pfad)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // nach erfolgreichem Umbenennen ohne Wirkung
	if _, err := tmp.Write(inhalt); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), pfad)
}
