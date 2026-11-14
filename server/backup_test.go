package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Stylle/today-random-movie-web/server/internal/portablebackup"
)

func TestPortableBackupRestoresIntoDifferentDeployment(t *testing.T) {
	t.Setenv("USER", "sourceadmin")
	t.Setenv("PASSWORD", "source-password-strong")
	source := newApp(t.TempDir(), t.TempDir())
	defer source.db.Close()
	secret, e := seal(source.secret, "test-fnos-password")
	if e != nil {
		t.Fatal(e)
	}
	source.updateSettings(map[string]string{"fn_password": secret, "fn_server": "https://nas.example/v"})
	source.exec("INSERT INTO movies(id,source_id,title,updated_at) VALUES(?,?,?,?)", "movie", "source", "Test", nowISO())
	if e = os.WriteFile(filepath.Join(source.dataDir, "posters", "movie"), []byte("cached-poster"), 0600); e != nil {
		t.Fatal(e)
	}
	archive := filepath.Join(t.TempDir(), "backup.zip")
	if e = portablebackup.Export(source.dataDir, source.db, backupSpec, archive); e != nil {
		t.Fatal(e)
	}
	targetDir := t.TempDir()
	t.Setenv("USER", "targetadmin")
	t.Setenv("PASSWORD", "target-password-strong")
	target := newApp(targetDir, t.TempDir())
	target.db.Close()
	if e = portablebackup.Stage(targetDir, archive, backupSpec, validateMovieBackup); e != nil {
		t.Fatal(e)
	}
	restored := newApp(targetDir, t.TempDir())
	defer restored.db.Close()
	if restored.one("SELECT id FROM users WHERE username='sourceadmin'") == nil || restored.one("SELECT id FROM users WHERE username='targetadmin'") != nil {
		t.Fatal("backup account must replace deployment account")
	}
	if restored.count("SELECT COUNT(*) FROM movies") != 1 {
		t.Fatal("movie index lost")
	}
	opened, e := openSealed(restored.secret, restored.settings()["fn_password"])
	if e != nil || opened != "test-fnos-password" {
		t.Fatal("restored credentials are not decryptable")
	}
	if data, e := os.ReadFile(filepath.Join(targetDir, "posters", "movie")); e != nil || string(data) != "cached-poster" {
		t.Fatal("cache not restored")
	}
	previous, _ := filepath.Glob(filepath.Join(targetDir, "previous-*", "app.db"))
	if len(previous) != 1 {
		t.Fatal("previous deployment must be retained")
	}
}
