package content

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load reads every track under root. A track is a directory holding
// track.yaml; its exercises are the directories beneath it that hold an
// exercise.md. Structural problems are collected and returned together so a
// contributor sees every error in one run.
func Load(root fs.FS) (*Library, error) {
	lib := &Library{Exercises: map[string]*Exercise{}}
	var errs []error

	trackDirs, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, fmt.Errorf("read content root: %w", err)
	}
	for _, d := range trackDirs {
		if !d.IsDir() {
			continue
		}
		trackPath := path.Join(d.Name(), "track.yaml")
		raw, err := fs.ReadFile(root, trackPath)
		if errors.Is(err, fs.ErrNotExist) {
			// Directories without track.yaml are not tracks (e.g. shared assets).
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", trackPath, err))
			continue
		}
		var t Track
		if err := yaml.Unmarshal(raw, &t); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", trackPath, err))
			continue
		}
		if t.ID == "" {
			t.ID = d.Name()
		}
		if t.ID != d.Name() {
			errs = append(errs, fmt.Errorf("%s: track id %q must match directory name %q", trackPath, t.ID, d.Name()))
		}
		lib.Tracks = append(lib.Tracks, t)

		exErrs := loadExercises(root, d.Name(), lib)
		errs = append(errs, exErrs...)
	}
	// Deterministic order regardless of filesystem enumeration.
	sort.Slice(lib.Tracks, func(i, j int) bool { return lib.Tracks[i].ID < lib.Tracks[j].ID })

	errs = append(errs, lib.checkReferences()...)
	if len(errs) > 0 {
		return lib, errors.Join(errs...)
	}
	return lib, nil
}

// loadExercises walks a track directory and loads every exercise directory
// found beneath it, registering each in lib by id.
func loadExercises(root fs.FS, trackDir string, lib *Library) []error {
	var errs []error
	walk := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "exercise.md" {
			return nil
		}
		dir := path.Dir(p)
		ex, exErrs := loadExercise(root, dir, trackDir)
		if len(exErrs) > 0 {
			errs = append(errs, exErrs...)
			return nil
		}
		if _, dup := lib.Exercises[ex.ID]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate exercise id %q", dir, ex.ID))
			return nil
		}
		lib.Exercises[ex.ID] = ex
		return nil
	}
	if err := fs.WalkDir(root, trackDir, walk); err != nil {
		errs = append(errs, fmt.Errorf("walk %s: %w", trackDir, err))
	}
	return errs
}

// loadExercise reads one exercise directory. The exercise id defaults to
// "<track>/<dirname>" and must carry the track prefix if set explicitly.
func loadExercise(root fs.FS, dir, trackDir string) (*Exercise, []error) {
	var errs []error
	fail := func(file string, err error) {
		errs = append(errs, fmt.Errorf("%s: %w", path.Join(dir, file), err))
	}

	md, err := fs.ReadFile(root, path.Join(dir, "exercise.md"))
	if err != nil {
		fail("exercise.md", err)
		return nil, errs
	}
	fm, body, err := splitFrontMatter(md)
	if err != nil {
		fail("exercise.md", err)
		return nil, errs
	}
	var meta frontMatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		fail("exercise.md", fmt.Errorf("front matter: %w", err))
		return nil, errs
	}
	ex := &Exercise{
		ID:         meta.ID,
		Title:      meta.Title,
		Concepts:   meta.Concepts,
		Syllabus:   meta.Syllabus,
		Difficulty: meta.Difficulty,
		Statement:  strings.TrimSpace(string(body)),
		Dir:        dir,
	}
	if ex.ID == "" {
		ex.ID = trackDir + "/" + path.Base(dir)
	}
	if !strings.HasPrefix(ex.ID, trackDir+"/") {
		fail("exercise.md", fmt.Errorf("id %q must start with %q", ex.ID, trackDir+"/"))
	}
	if ex.Title == "" {
		fail("exercise.md", errors.New("title is required"))
	}
	if ex.Difficulty < 1 || ex.Difficulty > 3 {
		fail("exercise.md", fmt.Errorf("difficulty %d must be 1..3", ex.Difficulty))
	}

	if b, err := fs.ReadFile(root, path.Join(dir, "starter.py")); err != nil {
		fail("starter.py", err)
	} else {
		ex.Starter = string(b)
	}
	if b, err := fs.ReadFile(root, path.Join(dir, "solution.py")); err != nil {
		fail("solution.py", err)
	} else {
		ex.Solution = string(b)
	}

	if b, err := fs.ReadFile(root, path.Join(dir, "tests.yaml")); err != nil {
		fail("tests.yaml", err)
	} else if err := yaml.Unmarshal(b, &ex.Tests); err != nil {
		fail("tests.yaml", err)
	} else {
		errs = append(errs, checkTests(dir, &ex.Tests)...)
	}

	// hints.yaml is optional: an exercise may rely on the model alone.
	if b, err := fs.ReadFile(root, path.Join(dir, "hints.yaml")); err == nil {
		var hf hintsFile
		if err := yaml.Unmarshal(b, &hf); err != nil {
			fail("hints.yaml", err)
		} else {
			ex.Hints = hf.Hints
			errs = append(errs, checkHints(dir, ex)...)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		fail("hints.yaml", err)
	}
	return ex, errs
}

// checkTests enforces the invariants the grader relies on: at least one case,
// unique ids, a sane time limit, and at least one visible case so the student
// always has an example to reason from.
func checkTests(dir string, t *Tests) []error {
	var errs []error
	f := path.Join(dir, "tests.yaml")
	if len(t.Cases) == 0 {
		errs = append(errs, fmt.Errorf("%s: at least one case is required", f))
	}
	if t.TimeLimitMS == 0 {
		t.TimeLimitMS = 2000
	}
	if t.TimeLimitMS < 100 || t.TimeLimitMS > 5000 {
		errs = append(errs, fmt.Errorf("%s: time_limit_ms %d must be 100..5000", f, t.TimeLimitMS))
	}
	seen := map[string]bool{}
	visible := false
	for i, c := range t.Cases {
		if c.ID == "" {
			errs = append(errs, fmt.Errorf("%s: case %d has no id", f, i))
			continue
		}
		if seen[c.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate case id %q", f, c.ID))
		}
		seen[c.ID] = true
		if !c.Hidden {
			visible = true
		}
	}
	if len(t.Cases) > 0 && !visible {
		errs = append(errs, fmt.Errorf("%s: at least one case must be visible", f))
	}
	return errs
}

// validOutcomes are the attempt outcomes a hint may be scoped to.
var validOutcomes = map[string]bool{"failed": true, "runtime_error": true, "syntax_error": true, "timeout": true}

// checkHints compiles message regexes and verifies each hint has an id, at
// least one matcher, some text, and refers only to test ids that exist.
// Flag names are checked later against the runner's known set.
func checkHints(dir string, ex *Exercise) []error {
	var errs []error
	f := path.Join(dir, "hints.yaml")
	cases := map[string]bool{}
	for _, c := range ex.Tests.Cases {
		cases[c.ID] = true
	}
	seen := map[string]bool{}
	for i := range ex.Hints {
		h := &ex.Hints[i]
		if h.ID == "" {
			errs = append(errs, fmt.Errorf("%s: hint %d has no id", f, i))
			continue
		}
		if seen[h.ID] {
			errs = append(errs, fmt.Errorf("%s: duplicate hint id %q", f, h.ID))
		}
		seen[h.ID] = true
		m := &h.Match
		if m.Outcome == "" && m.Exception == "" && m.Message == "" && m.FailingTest == "" && m.Flag == "" {
			errs = append(errs, fmt.Errorf("%s: hint %q has no matcher", f, h.ID))
		}
		if m.Outcome != "" && !validOutcomes[m.Outcome] {
			errs = append(errs, fmt.Errorf("%s: hint %q has unknown outcome %q", f, h.ID, m.Outcome))
		}
		if m.Message != "" {
			re, err := regexp.Compile(m.Message)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: hint %q message regex: %w", f, h.ID, err))
			}
			m.messageRE = re
		}
		if m.FailingTest != "" && !cases[m.FailingTest] {
			errs = append(errs, fmt.Errorf("%s: hint %q refers to unknown test %q", f, h.ID, m.FailingTest))
		}
		if h.Text.Hinglish == "" && h.Text.En == "" {
			errs = append(errs, fmt.Errorf("%s: hint %q has no text", f, h.ID))
		}
	}
	return errs
}

// checkReferences verifies every exercise a track lists exists and that
// must_pass ids are among the unit's exercises.
func (l *Library) checkReferences() []error {
	var errs []error
	for _, t := range l.Tracks {
		unitIDs := map[string]bool{}
		for _, u := range t.Units {
			if u.ID == "" {
				errs = append(errs, fmt.Errorf("track %s: unit without id", t.ID))
				continue
			}
			if unitIDs[u.ID] {
				errs = append(errs, fmt.Errorf("track %s: duplicate unit id %q", t.ID, u.ID))
			}
			unitIDs[u.ID] = true
			listed := map[string]bool{}
			for _, id := range u.Exercises {
				listed[id] = true
				if _, ok := l.Exercises[id]; !ok {
					errs = append(errs, fmt.Errorf("track %s unit %s: unknown exercise %q", t.ID, u.ID, id))
				}
			}
			for _, id := range u.MustPass {
				if !listed[id] {
					errs = append(errs, fmt.Errorf("track %s unit %s: must_pass %q is not in the unit's exercises", t.ID, u.ID, id))
				}
			}
		}
	}
	return errs
}

// CheckFlags reports hints whose flag is not in known; the harness's flag set
// is owned by the runner, so the loader takes it as a parameter.
func (l *Library) CheckFlags(known map[string]bool) []error {
	var errs []error
	for _, ex := range l.Exercises {
		for _, h := range ex.Hints {
			if h.Match.Flag != "" && !known[h.Match.Flag] {
				errs = append(errs, fmt.Errorf("%s/hints.yaml: hint %q uses unknown flag %q", ex.Dir, h.ID, h.Match.Flag))
			}
		}
	}
	return errs
}

var frontMatterDelim = []byte("---")

// splitFrontMatter separates the leading "---" YAML block from the Markdown
// body. The block is required so every exercise carries its metadata.
func splitFrontMatter(md []byte) (fm, body []byte, err error) {
	md = bytes.TrimLeft(md, "\xef\xbb\xbf") // strip a UTF-8 BOM if present
	if !bytes.HasPrefix(md, frontMatterDelim) {
		return nil, nil, errors.New("missing front matter")
	}
	rest := md[len(frontMatterDelim):]
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return nil, nil, errors.New("unterminated front matter")
	}
	fm = rest[:end]
	body = rest[end+len("\n---"):]
	return fm, body, nil
}
