package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func storageFixture(t *testing.T) (*nativeEngine, string) {
	t.Helper()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	id := nativeDownloadID("hongguo:70001", 1)
	manager := engine.downloads
	if err := os.MkdirAll(filepath.Join(manager.root, id), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manager.root, id, "media.mp4"), []byte("synthetic-video"), 0600); err != nil {
		t.Fatal(err)
	}
	manager.jobs[id] = &nativeDownloadRecord{nativeDownloadJob: nativeDownloadJob{
		ID: id, Index: 1, Drama: nativeDrama{ID: "hongguo:70001", Source: "hongguo"},
		State: "completed", Bytes: 15}, File: "media.mp4"}
	if err := manager.saveLocked(); err != nil {
		t.Fatal(err)
	}
	return engine, id
}

func TestDownloadMigrationPreservesMediaAndRestartLocation(t *testing.T) {
	engine, id := storageFixture(t)
	old := engine.downloads.root
	target := t.TempDir()
	if err := engine.moveDownloads(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	location := nativeDownloadLocation(engine.directory)
	if location != engine.downloads.root || location == old {
		t.Fatal("new location was not persisted")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old media not cleaned after successful migration")
	}
	if body, err := os.ReadFile(filepath.Join(location, id, "media.mp4")); err != nil || string(body) != "synthetic-video" {
		t.Fatalf("media not copied: %v", err)
	}
	info, err := engine.storage()
	if err != nil || info.Files < 2 || info.Bytes < 15 || info.Free < 1 {
		t.Fatalf("storage stats: %+v %v", info, err)
	}
	restarted := newNativeDownloads(engine)
	defer restarted.close()
	if len(restarted.jobs) != 1 || restarted.jobs[id].State != "completed" {
		t.Fatal("restart lost jobs")
	}
}

func TestMigrationFailureLeavesSourceAndExistingTargetIntact(t *testing.T) {
	engine, _ := storageFixture(t)
	old := engine.downloads.root
	parent := t.TempDir()
	existing := filepath.Join(parent, "zhenguojian-downloads")
	os.Mkdir(existing, 0700)
	os.WriteFile(filepath.Join(existing, "user-file"), []byte("keep"), 0600)
	if err := engine.moveDownloads(context.Background(), parent); err == nil {
		t.Fatal("existing directory overwritten")
	}
	if _, err := os.Stat(filepath.Join(existing, "user-file")); err != nil {
		t.Fatal("existing content removed")
	}
	if engine.downloads.root != old {
		t.Fatal("failed migration changed root")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	empty := t.TempDir()
	if err := engine.moveDownloads(ctx, empty); err == nil {
		t.Fatal("cancelled migration succeeded")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("source removed after cancellation")
	}
	if _, err := os.Stat(filepath.Join(empty, "zhenguojian-downloads")); !os.IsNotExist(err) {
		t.Fatal("partial target not cleaned")
	}
}

func TestMediaLeasePreventsMigrationAndDeletingSourceFiles(t *testing.T) {
	engine, id := storageFixture(t)
	if _, err := engine.workLease("media", "start"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.workLease("media", "start"); err == nil {
		t.Fatal("parallel media operations accepted")
	}
	if err := engine.moveDownloads(context.Background(), t.TempDir()); err == nil {
		t.Fatal("moving busy media")
	}
	if err := engine.downloads.control(id, "remove"); err == nil {
		t.Fatal("deleting active input")
	}
	if _, err := engine.workLease("media", "end"); err != nil {
		t.Fatal(err)
	}
	if err := engine.downloads.control(id, "remove"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationRejectsContainmentAndSymbolicLinks(t *testing.T) {
	engine, _ := storageFixture(t)
	if err := engine.moveDownloads(context.Background(), engine.downloads.root); err == nil {
		t.Fatal("nested destination accepted")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(engine.downloads.root, "link")); err != nil {
		t.Skip(err)
	}
	if err := engine.moveDownloads(context.Background(), t.TempDir()); err == nil {
		t.Fatal("followed symlink")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("removed linked directory")
	}
}
