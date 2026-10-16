package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rolloutSeg is one physical file of a Desktop thread. id is the thread id
// for the root file and the filename suffix for a continuation. Children
// point at that id via session_meta.payload.history_base.thread_id.
type rolloutSeg struct {
	path     string
	name     string
	id       string
	parentID string
}

// findRollout returns the segment Codex is writing now. Walk order hits the
// oldest file first; continuations live in later rollout-<time>-<threadID>_<segmentID>.jsonl
// files, and the live one is the leaf of the history_base chain.
func findRollout(threadID string) (string, error) {
	segs, err := threadRollouts(threadID)
	if err != nil {
		return "", err
	}
	cur, ok := currentRollout(segs)
	if !ok {
		return "", fmt.Errorf("codex desktop thread %s has no rollout yet; create it with send --surface app first", threadID)
	}
	return cur.path, nil
}

func codexSessionsDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

func threadRollouts(threadID string) ([]rolloutSeg, error) {
	if strings.TrimSpace(threadID) == "" {
		return nil, nil
	}
	root, err := codexSessionsDir()
	if err != nil {
		return nil, err
	}
	var segs []rolloutSeg
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !isThreadRollout(name, threadID) {
			return nil
		}
		segs = append(segs, rolloutSeg{
			path:     path,
			name:     name,
			id:       rolloutSegmentID(name, threadID),
			parentID: rolloutParentID(path),
		})
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		return nil, walkErr
	}
	return segs, nil
}

func isThreadRollout(name, threadID string) bool {
	return strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") && strings.Contains(name, threadID)
}

func rolloutSegmentID(name, threadID string) string {
	base := strings.TrimSuffix(name, ".jsonl")
	marker := threadID + "_"
	if i := strings.LastIndex(base, marker); i >= 0 {
		if id := base[i+len(marker):]; id != "" {
			return id
		}
	}
	return threadID
}

func rolloutParentID(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for i := 0; i < 4 && sc.Scan(); i++ {
		parent, ok := historyBaseThreadID(sc.Bytes())
		if ok {
			return parent
		}
	}
	return ""
}

func historyBaseThreadID(line []byte) (string, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return "", false
	}
	var event struct {
		Type    string `json:"type"`
		Payload struct {
			HistoryBase *struct {
				ThreadID string `json:"thread_id"`
			} `json:"history_base"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return "", false
	}
	if event.Type != "session_meta" {
		return "", false
	}
	if event.Payload.HistoryBase == nil {
		return "", true
	}
	return event.Payload.HistoryBase.ThreadID, true
}

// currentRollout is the segment nothing else continues. A fork picks the
// newest filename; the timestamp in rollout-<time>-... matches creation order.
func currentRollout(segs []rolloutSeg) (rolloutSeg, bool) {
	if len(segs) == 0 {
		return rolloutSeg{}, false
	}
	continued := map[string]bool{}
	for _, seg := range segs {
		if seg.parentID != "" {
			continued[seg.parentID] = true
		}
	}
	leaves := make([]rolloutSeg, 0, len(segs))
	for _, seg := range segs {
		if !continued[seg.id] {
			leaves = append(leaves, seg)
		}
	}
	if len(leaves) == 0 {
		leaves = append(leaves, segs...)
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].name < leaves[j].name })
	return leaves[len(leaves)-1], true
}

// readActiveRollout reads the anchor segment plus any continuation opened
// after the before-snapshot. Ancestor files stay out of the scan: their user
// turns are not in the snapshot and would look like a second copy of the prompt.
func readActiveRollout(threadID, anchor string) ([]byte, error) {
	paths := []string{anchor}
	if threadID != "" {
		segs, err := threadRollouts(threadID)
		if err != nil {
			return nil, err
		}
		if len(segs) > 0 {
			paths = rolloutSpan(anchor, segs)
		}
	}
	if len(paths) == 0 {
		paths = []string{anchor}
	}
	if len(paths) == 1 {
		return os.ReadFile(paths[0])
	}
	return readRolloutSpan(paths)
}

func rolloutSpan(anchor string, segs []rolloutSeg) []string {
	children := map[string][]int{}
	anchorIdx := -1
	for i := range segs {
		if segs[i].parentID != "" {
			children[segs[i].parentID] = append(children[segs[i].parentID], i)
		}
		if segs[i].path == anchor {
			anchorIdx = i
		}
	}
	if anchorIdx < 0 {
		if cur, ok := currentRollout(segs); ok {
			return []string{cur.path}
		}
		return []string{anchor}
	}
	paths := []string{segs[anchorIdx].path}
	id := segs[anchorIdx].id
	seen := map[string]bool{id: true}
	for {
		kids := children[id]
		if len(kids) == 0 {
			return paths
		}
		best := kids[0]
		for _, idx := range kids[1:] {
			if segs[idx].name > segs[best].name {
				best = idx
			}
		}
		next := segs[best]
		if next.id == "" || seen[next.id] {
			return paths
		}
		seen[next.id] = true
		paths = append(paths, next.path)
		id = next.id
	}
}

func readRolloutSpan(paths []string) ([]byte, error) {
	seen := map[string]bool{}
	var out bytes.Buffer
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		filtered := dropSeenUserMessages(data, seen)
		if len(filtered) == 0 {
			continue
		}
		if out.Len() > 0 && out.Bytes()[out.Len()-1] != '\n' {
			out.WriteByte('\n')
		}
		out.Write(filtered)
	}
	return out.Bytes(), nil
}

// dropSeenUserMessages removes a resent parent-tail user message so the same
// id is not counted twice. It uses the same message/user filter as userTurns.
func dropSeenUserMessages(data []byte, seen map[string]bool) []byte {
	if len(data) == 0 {
		return data
	}
	lines := bytes.Split(data, []byte("\n"))
	kept := make([][]byte, 0, len(lines))
	changed := false
	for _, line := range lines {
		id := userMessageID(line)
		if id != "" {
			if seen[id] {
				changed = true
				continue
			}
			seen[id] = true
		}
		kept = append(kept, line)
	}
	if !changed {
		return data
	}
	return bytes.Join(kept, []byte("\n"))
}

func userMessageID(line []byte) string {
	turns := userTurns(line)
	if len(turns) != 1 {
		return ""
	}
	return turns[0].messageID
}
