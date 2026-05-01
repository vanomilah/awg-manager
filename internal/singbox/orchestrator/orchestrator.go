package orchestrator

import (
	"context"
	"fmt"
	"sync"
)

// ProcessController is the subset of sing-box.Process that orchestrator
// uses to manage lifecycle and reload. Full Process satisfies it.
type ProcessController interface {
	IsRunning() (bool, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Reload() error // SIGHUP to running process
}

// Orchestrator is the single writer for sing-box config.d. See package
// doc. Safe for concurrent use by registered producers.
type Orchestrator struct {
	configDir string
	proc      ProcessController

	mu      sync.Mutex
	slots   map[Slot]SlotMeta
	enabled map[Slot]bool

	// dirty signals that on-disk state changed since the last successful
	// reload. Task 4 wires this into a debounced reloader. For now Save
	// / SetEnabled just flip it.
	dirty bool
}

// New constructs an orchestrator rooted at configDir (typically
// /opt/etc/sing-box/config.d). It does NOT touch disk — call Bootstrap
// after construction to scan/migrate existing files.
func New(configDir string, proc ProcessController) *Orchestrator {
	return &Orchestrator{
		configDir: configDir,
		proc:      proc,
		slots:     make(map[Slot]SlotMeta),
		enabled:   make(map[Slot]bool),
	}
}

// Register adds a slot to the registry. Returns ErrSlotAlreadyRegistered
// if called twice for the same slot.
func (o *Orchestrator) Register(meta SlotMeta) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.slots[meta.Slot]; ok {
		return ErrSlotAlreadyRegistered
	}
	o.slots[meta.Slot] = meta
	if meta.AlwaysOn {
		o.enabled[meta.Slot] = true
	}
	return nil
}

// Bootstrap ensures the on-disk layout (configDir + disabled subdir)
// exists and populates the in-memory enabled map for any registered
// slot whose file is found. Call once after all Register calls and
// before any Save/SetEnabled. Idempotent.
func (o *Orchestrator) Bootstrap() error {
	if err := o.ensureDirs(); err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for slot, meta := range o.slots {
		a, d := o.scanDirForSlot(meta)
		switch {
		case a && d:
			// Pathological: same slot in both places. Prefer active as
			// truth, drop the stale disabled copy.
			if err := o.removeDisabledCopy(meta); err != nil {
				return fmt.Errorf("bootstrap %s: %w", slot, err)
			}
			o.enabled[slot] = true
		case a:
			o.enabled[slot] = true
		case d:
			// File exists only in disabled/ — slot stays disabled.
			o.enabled[slot] = false
		default:
			// No file. AlwaysOn stays true (the producer must Save
			// its initial content); regular slots default to false.
			if !meta.AlwaysOn {
				o.enabled[slot] = false
			}
		}
	}
	return nil
}

// removeDisabledCopy deletes the disabled-side file for a slot. Used
// only to resolve a both-locations conflict during Bootstrap.
func (o *Orchestrator) removeDisabledCopy(meta SlotMeta) error {
	return removeIfExists(o.disabledPath(meta))
}

// Save writes the slot's JSON atomically to whichever location matches
// the slot's CURRENT enabled state. Marks the orchestrator dirty so a
// later Reload (Task 4) will pick it up.
func (o *Orchestrator) Save(slot Slot, jsonBytes []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	meta, ok := o.slots[slot]
	if !ok {
		return ErrUnknownSlot
	}
	var path string
	if o.enabled[slot] {
		path = o.activePath(meta)
	} else {
		path = o.disabledPath(meta)
	}
	if err := writeAtomic(path, jsonBytes); err != nil {
		return fmt.Errorf("save %s: %w", slot, err)
	}
	o.dirty = true
	return nil
}

// SetEnabled toggles slot activity by renaming the file between
// active and disabled locations. AlwaysOn slots reject disable.
// Marks the orchestrator dirty.
func (o *Orchestrator) SetEnabled(slot Slot, enabled bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	meta, ok := o.slots[slot]
	if !ok {
		return ErrUnknownSlot
	}
	if !enabled && meta.AlwaysOn {
		return ErrSlotAlwaysOn
	}
	if o.enabled[slot] == enabled {
		return nil // already in target state
	}
	if err := o.renameForToggle(meta, enabled); err != nil {
		return fmt.Errorf("toggle %s: %w", slot, err)
	}
	o.enabled[slot] = enabled
	o.dirty = true
	return nil
}

// Snapshot returns the current state of all registered slots in
// KnownSlots() order, filtering to only those that are registered.
func (o *Orchestrator) Snapshot() []SlotState {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []SlotState
	for _, meta := range KnownSlots() {
		if _, ok := o.slots[meta.Slot]; !ok {
			continue
		}
		en := o.enabled[meta.Slot]
		var path string
		if en {
			path = o.activePath(meta)
		} else {
			path = o.disabledPath(meta)
		}
		out = append(out, SlotState{
			Slot:     meta.Slot,
			Filename: meta.Filename,
			Enabled:  en,
			Present:  fileExists(path),
			Bytes:    fileSize(path),
		})
	}
	return out
}

// Reload — implemented in Task 4.
func (o *Orchestrator) Reload() error {
	return nil
}
