package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

const (
	maxAgentLoopItems          = 25
	defaultAutoLoopIterations  = 5
	maxAutoLoopIterationsLimit = 10
)

var (
	staticImportSpecifierPattern = regexp.MustCompile(`\bfrom\s+["']([^"']+)["']|^\s*import\s+["']([^"']+)["']`)
	namedImportPattern           = regexp.MustCompile(`\{([^}]*)\}`)
	reactMemberPattern           = regexp.MustCompile(`\bReact\.([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsxTextPattern               = regexp.MustCompile(`>\s*([^<>{}\n][^<>{}]*)\s*<`)
	createElementTextPattern     = regexp.MustCompile(`React\.createElement\([^)]*,\s*[^,)]*,\s*["']([^"']+)["']`)
)

func (r *Runtime) ListAgentLoops(context.Context) []AgentLoop {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.agentLoops) == 0 {
		return []AgentLoop{}
	}
	return append([]AgentLoop(nil), r.agentLoops...)
}

func (r *Runtime) StartAgentLoop(ctx context.Context, input AgentLoopInput) (AgentLoop, error) {
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return AgentLoop{}, errors.New("Goal is required.")
	}
	now := time.Now().UTC()
	mode := normalizeAgentLoopMode(input.Mode)
	loop := AgentLoop{
		ID:            newTraceID(),
		Goal:          trimRunes(goal, 280),
		Mode:          mode,
		State:         "running",
		MaxIterations: normalizeAgentLoopIterations(mode, input.MaxIterations),
		AutoApply:     input.AutoApply,
		SessionID:     strings.TrimSpace(input.SessionID),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "plan",
		Title:  "Understand request",
		State:  "completed",
		Detail: loopPlanDetail(mode),
	})
	if input.TempWorkspace || shouldUseTempAppWorkspace(goal) {
		if loop.Mode != "auto" {
			loop.Mode = "auto"
			loop.MaxIterations = normalizeAgentLoopIterations(loop.Mode, input.MaxIterations)
		}
		loop.AutoApply = loop.AutoApply || input.AutoApply
		loop = r.runTempAppLoop(ctx, loop)
	} else {
		loop = r.runLoopSteps(ctx, loop, input)
	}
	if loop.State == "running" {
		loop.State = "completed"
	}
	loop.UpdatedAt = time.Now().UTC()
	loop.Summary = loopSummary(loop)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentLoops = append([]AgentLoop{loop}, r.agentLoops...)
	if len(r.agentLoops) > maxAgentLoopItems {
		r.agentLoops = r.agentLoops[:maxAgentLoopItems]
	}
	return loop, nil
}

func (r *Runtime) runTempAppLoop(ctx context.Context, loop AgentLoop) AgentLoop {
	sessionID := strings.TrimSpace(loop.SessionID)
	if sessionID == "" {
		sessionID = loop.ID
		loop.SessionID = sessionID
	}
	session, hasSession := r.appSession(sessionID)
	replacingSession := hasSession && shouldStartFreshTempAppWorkspace(loop.Goal)
	if !hasSession || replacingSession {
		dir, err := os.MkdirTemp("", "linea-app-*")
		if err != nil {
			return appendLoopStep(loop, "workspace", "Create temp workspace", "workspace", err, "", "")
		}
		session = AppSession{ID: sessionID, Root: dir}
	}
	if session.ID == "" {
		session.ID = sessionID
	}
	if session.ID == "" {
		session.ID = loop.ID
	}
	loop.WorkspaceRoot = session.Root
	action := "Create temp package"
	editing := hasSession && !replacingSession
	if editing {
		action = "Reuse temp package"
	}
	loop = appendLoopStep(loop, "workspace", action, "workspace", nil, session.Root, "")

	previousApp, hadPreviousApp := readTempAppSource(session.Root)
	if err := r.writeTempAppProject(ctx, session.Root, loop.Goal, editing); err != nil {
		return appendLoopStep(loop, "write_file", "Write app package", "edit_file", err, "package files", "")
	}
	loop = appendLoopStep(loop, "write_file", "Write app package", "edit_file", nil, "package.json, index.html, src", "")
	if err := validateTempAppProject(session.Root); err != nil {
		restoreTempAppSource(session.Root, previousApp, hadPreviousApp)
		return appendLoopStep(loop, "app_check", "Check app", "workspace", err, "src/App.jsx", "")
	}
	loop = appendLoopStep(loop, "app_check", "Check app", "workspace", nil, "ok", "")
	if err := buildTempAppProject(session.Root); err != nil {
		restoreTempAppSource(session.Root, previousApp, hadPreviousApp)
		return appendLoopStep(loop, "app_build", "Build preview", "workspace", err, "dist", "")
	}
	loop = appendLoopStep(loop, "app_build", "Build preview", "workspace", nil, "ok", "")

	snapshotRoot, err := snapshotPreviewBuild(session.Root)
	if err != nil {
		restoreTempAppSource(session.Root, previousApp, hadPreviousApp)
		return appendLoopStep(loop, "preview", "Create preview", "workspace", err, "dist", "")
	}
	r.saveAppSession(session)
	preview := r.registerAgentPreview(loop.ID, session.ID, snapshotRoot, "index.html")
	loop.PreviewURL = preview.URL
	loop = appendLoopStep(loop, "preview", "Create preview", "workspace", nil, preview.URL, "")

	select {
	case <-ctx.Done():
		return appendLoopStep(loop, "cancel", "Finish loop", "workspace", ctx.Err(), "", "")
	default:
	}
	return loop
}

func readTempAppSource(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, "src", "App.jsx"))
	if err != nil {
		return "", false
	}
	return string(data), true
}

func restoreTempAppSource(root string, content string, exists bool) {
	path := filepath.Join(root, "src", "App.jsx")
	if !exists {
		_ = os.Remove(path)
		return
	}
	_ = os.WriteFile(path, []byte(content), 0o644)
}

func (r *Runtime) writeTempAppProject(ctx context.Context, root string, goal string, editing bool) error {
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"package.json":               tempAppPackageJSON(),
		"index.html":                 tempAppIndexHTML(),
		"src/main.jsx":               tempAppMainJSX(),
		"vendor/react.js":            tempAppReactShim(),
		"vendor/react-dom-client.js": tempAppReactDOMShim(),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	appPath := filepath.Join(root, "src", "App.jsx")
	current := ""
	if editing {
		data, err := os.ReadFile(appPath)
		if err == nil {
			current = string(data)
		}
	}
	content, err := r.planTempAppContent(ctx, goal, current)
	if err != nil {
		return err
	}
	return os.WriteFile(appPath, []byte(content), 0o644)
}

func (r *Runtime) planTempAppContent(ctx context.Context, goal string, current string) (string, error) {
	plannerTried := false
	if r.editPlanner != nil {
		plannerTried = true
		files := []FileResult{{Path: "src/App.jsx", Content: current, Size: int64(len(current))}}
		plan, err := r.editPlanner.PlanEdit(ctx, EditPlanRequest{Goal: goal, Files: files})
		if err == nil && browserRunnableReactModule(plan.Content) {
			return strings.TrimSpace(plan.Content) + "\n", nil
		}
	}
	if message, ok := tempAppMessage(goal); ok {
		return tempMessageAppJSX(message), nil
	}
	if strings.TrimSpace(current) != "" && shouldMakeAppLouder(goal) {
		return tempLouderAppJSX(), nil
	}
	if strings.TrimSpace(current) != "" {
		if color, ok := tempAppColorGoal(goal); ok {
			return tempColorAppJSX(tempAppDisplayText(current), color), nil
		}
	}
	if strings.TrimSpace(current) != "" {
		return "", errors.New("Could not plan a supported temp app edit.")
	}
	if plannerTried {
		return "", errors.New("Could not plan a supported temp app.")
	}
	if !isGenericTempAppGoal(goal) {
		return "", errors.New("Could not plan a supported temp app.")
	}
	return tempDefaultAppJSX(), nil
}

func snapshotPreviewBuild(root string) (string, error) {
	source := filepath.Join(root, "dist")
	target, err := os.MkdirTemp("", "linea-preview-*")
	if err != nil {
		return "", err
	}
	if err := copyDir(source, target); err != nil {
		return "", err
	}
	return target, nil
}

func copyDir(source string, target string) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		out := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}

func validateTempAppProject(root string) error {
	appData, err := os.ReadFile(filepath.Join(root, "src", "App.jsx"))
	if err != nil {
		return err
	}
	mainData, err := os.ReadFile(filepath.Join(root, "src", "main.jsx"))
	if err != nil {
		return err
	}
	app := string(appData)
	main := string(mainData)
	if !strings.Contains(app, "export default") {
		return errors.New("src/App.jsx must export App")
	}
	if !hasReactImport(app) {
		return errors.New("src/App.jsx must import React")
	}
	if err := validateTempAppImports(app); err != nil {
		return err
	}
	if err := validateReactShimMembers(app); err != nil {
		return err
	}
	if !strings.Contains(main, "createRoot") {
		return errors.New("src/main.jsx must mount React")
	}
	if !looksLikeSupportedReactModule(app) {
		return errors.New("src/App.jsx must use a supported default export")
	}
	checkDir, err := os.MkdirTemp("", "linea-preview-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(checkDir)
	if err := bundleTempAppProject(root, checkDir); err != nil {
		return err
	}
	return nil
}

func looksLikeSupportedReactModule(app string) bool {
	return strings.Contains(app, "export default function App(") ||
		(strings.Contains(app, "function App(") && strings.Contains(app, "export default App")) ||
		(strings.Contains(app, "const App") && strings.Contains(app, "export default App"))
}

func hasReactImport(app string) bool {
	for _, line := range strings.Split(app, "\n") {
		specifiers := staticImportSpecifiers(line)
		if len(specifiers) == 0 || specifiers[0] != "react" {
			continue
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "import React") {
			return true
		}
		if strings.HasPrefix(line, "import * as React") {
			return true
		}
	}
	return false
}

func validateTempAppImports(app string) error {
	for _, line := range strings.Split(app, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "import ") {
			continue
		}
		specifiers := staticImportSpecifiers(line)
		if len(specifiers) == 0 {
			return errors.New("src/App.jsx imports must use static string specifiers")
		}
		for _, specifier := range specifiers {
			switch specifier {
			case "react":
				if err := validateReactShimImport(line); err != nil {
					return err
				}
				continue
			case "react-dom/client":
				continue
			default:
				if strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../") {
					if err := ensureTempAppLocalImport(specifier); err != nil {
						return err
					}
				}
				continue
			}
		}
	}
	return nil
}

func ensureTempAppLocalImport(specifier string) error {
	cleaned := filepath.Clean(filepath.Join("src", specifier))
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return fmt.Errorf("src/App.jsx cannot import outside src: %s", specifier)
	}
	extension := strings.ToLower(filepath.Ext(cleaned))
	switch extension {
	case ".css", ".js", ".jsx", ".mjs":
		return nil
	default:
		return fmt.Errorf("src/App.jsx cannot import unsupported local file %q", specifier)
	}
}

func validateReactShimImport(line string) error {
	match := namedImportPattern.FindStringSubmatch(line)
	if len(match) == 0 {
		return nil
	}
	for _, item := range strings.Split(match[1], ",") {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if before, _, ok := strings.Cut(name, " as "); ok {
			name = strings.TrimSpace(before)
		}
		switch name {
		case "Fragment", "useEffect", "useMemo", "useRef", "useState", "createElement":
			continue
		default:
			return fmt.Errorf("src/App.jsx cannot import unsupported React export %q", name)
		}
	}
	return nil
}

func validateReactShimMembers(app string) error {
	for _, match := range reactMemberPattern.FindAllStringSubmatch(app, -1) {
		if len(match) < 2 {
			continue
		}
		switch match[1] {
		case "Fragment", "useEffect", "useMemo", "useRef", "useState", "createElement":
			continue
		default:
			return fmt.Errorf("src/App.jsx cannot use unsupported React API %q", "React."+match[1])
		}
	}
	return nil
}

func staticImportSpecifiers(line string) []string {
	matches := staticImportSpecifierPattern.FindAllStringSubmatch(strings.TrimSpace(line), -1)
	specifiers := make([]string, 0, len(matches))
	for _, matchSet := range matches {
		for _, match := range matchSet[1:] {
			if match != "" {
				specifiers = append(specifiers, match)
				break
			}
		}
	}
	return specifiers
}

func buildTempAppProject(root string) error {
	dist := filepath.Join(root, "dist")
	if err := os.RemoveAll(dist); err != nil {
		return err
	}
	if err := os.MkdirAll(dist, 0o755); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "index.html"), filepath.Join(dist, "index.html")); err != nil {
		return err
	}
	if err := bundleTempAppProject(root, dist); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(root, "src"), filepath.Join(dist, "src")); err != nil {
		return err
	}
	return copyDir(filepath.Join(root, "vendor"), filepath.Join(dist, "vendor"))
}

func bundleTempAppProject(root string, outdir string) error {
	if err := ensureImportedTempAppCSS(root); err != nil {
		return err
	}
	assetDir := filepath.Join(outdir, "assets")
	result := api.Build(api.BuildOptions{
		AbsWorkingDir: root,
		EntryPoints:   []string{filepath.Join(root, "src", "main.jsx")},
		Outfile:       filepath.Join(assetDir, "app.js"),
		Bundle:        true,
		Format:        api.FormatESModule,
		Platform:      api.PlatformBrowser,
		JSX:           api.JSXTransform,
		Loader: map[string]api.Loader{
			".css": api.LoaderCSS,
			".js":  api.LoaderJS,
			".jsx": api.LoaderJSX,
			".mjs": api.LoaderJS,
		},
		Plugins:  []api.Plugin{tempAppResolvePlugin(root)},
		Write:    true,
		LogLevel: api.LogLevelSilent,
	})
	if len(result.Errors) > 0 {
		return fmt.Errorf("temp app bundle failed: %s", result.Errors[0].Text)
	}
	cssPath := filepath.Join(assetDir, "app.css")
	if _, err := os.Stat(cssPath); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(cssPath, []byte(""), 0o644)
	} else if err != nil {
		return err
	}
	return nil
}

func tempAppResolvePlugin(root string) api.Plugin {
	return api.Plugin{
		Name: "linea-temp-app-resolve",
		Setup: func(build api.PluginBuild) {
			build.OnResolve(api.OnResolveOptions{Filter: `^react$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: filepath.Join(root, "vendor", "react.js")}, nil
			})
			build.OnResolve(api.OnResolveOptions{Filter: `^react-dom/client$`}, func(api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{Path: filepath.Join(root, "vendor", "react-dom-client.js")}, nil
			})
		},
	}
}

func ensureImportedTempAppCSS(root string) error {
	appPath := filepath.Join(root, "src", "App.jsx")
	data, err := os.ReadFile(appPath)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		for _, specifier := range staticImportSpecifiers(line) {
			if !(strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../")) || strings.ToLower(filepath.Ext(specifier)) != ".css" {
				continue
			}
			path := filepath.Clean(filepath.Join(filepath.Dir(appPath), specifier))
			rel, err := filepath.Rel(filepath.Join(root, "src"), path)
			if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
				return fmt.Errorf("src/App.jsx cannot import CSS outside src: %s", specifier)
			}
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(source string, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func browserRunnableReactModule(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" || !strings.Contains(content, "export default") {
		return false
	}
	return strings.Contains(content, "React.createElement") ||
		strings.Contains(content, "<")
}

func (r *Runtime) ContinueAgentLoop(ctx context.Context, id string, input AgentLoopContinueInput) (AgentLoop, error) {
	loop, err := r.agentLoopByID(id)
	if err != nil {
		return AgentLoop{}, err
	}
	if loop.State == "canceled" {
		return AgentLoop{}, errors.New("Agent loop is canceled.")
	}
	if loop.State == "completed" {
		return AgentLoop{}, errors.New("Agent loop is already completed.")
	}
	if loop.Mode == "auto" && input.MaxIterations != 0 {
		nextMax := normalizeAgentLoopIterations(loop.Mode, input.MaxIterations)
		if nextMax > loop.MaxIterations {
			loop.MaxIterations = nextMax
		}
	}
	if loop.Mode == "auto" && input.AutoApply {
		loop.AutoApply = true
	}
	if loop.Mode == "auto" && input.MaxIterations == 0 && autoLoopLimitReached(loop) && hasExplicitLoopContinueInput(input) {
		loop.MaxIterations = normalizeAgentLoopIterations(loop.Mode, loop.MaxIterations+1)
	}
	loop.State = "running"
	var blocked bool
	loop, blocked = r.consumeAppliedEditReviews(loop)
	if blocked {
		loop.UpdatedAt = time.Now().UTC()
		loop.Summary = loopSummary(loop)
		r.replaceAgentLoop(loop)
		return loop, nil
	}
	if (hasUnresolvedEditBoundary(loop) || hasUnresolvedEditReview(loop)) && strings.TrimSpace(input.ProposalPath) == "" {
		loop.State = "waiting_approval"
		loop.UpdatedAt = time.Now().UTC()
		loop.Summary = loopSummary(loop)
		r.replaceAgentLoop(loop)
		return loop, nil
	}
	if r.hasTempAppSession(loop) {
		if query := strings.TrimSpace(input.Query); query != "" {
			loop.Goal = trimRunes(query, 280)
		}
		loop = r.runTempAppLoop(ctx, loop)
		if loop.State == "running" {
			loop.State = "completed"
		}
		loop.UpdatedAt = time.Now().UTC()
		loop.Summary = loopSummary(loop)
		r.replaceAgentLoop(loop)
		return loop, nil
	}
	continued := false
	for index, step := range loop.Steps {
		if step.Kind != "command_approval" || step.State != "waiting_approval" || strings.TrimSpace(step.Command) == "" {
			continue
		}
		if hasUnresolvedEditBoundaryBefore(loop, index) {
			loop.State = "waiting_approval"
			loop.UpdatedAt = time.Now().UTC()
			loop.Summary = loopSummary(loop)
			r.replaceAgentLoop(loop)
			return loop, nil
		}
		approvalID := step.CreatedID
		if err := r.checkCommandApproval(step.Command, approvalID); err != nil {
			approvalID = r.approvedCommandApprovalID(step.Command)
		}
		loop.Steps[index].State = "completed"
		loop.Steps[index].Detail = "Approval consumed."
		loop = r.runLoopCommand(ctx, loop, step.Command, approvalID)
		continued = true
		break
	}
	if !continued {
		loop = r.runLoopSteps(ctx, loop, AgentLoopInput{
			Goal:            loop.Goal,
			Mode:            loop.Mode,
			MaxIterations:   firstNonZero(input.MaxIterations, loop.MaxIterations),
			AutoApply:       loop.AutoApply || input.AutoApply,
			TempWorkspace:   loop.WorkspaceRoot != "",
			Command:         input.Command,
			Query:           input.Query,
			FilePath:        input.FilePath,
			ProposalPath:    input.ProposalPath,
			ProposalContent: input.ProposalContent,
		})
	}
	if loop.State == "running" {
		if hasUnresolvedEditBoundary(loop) {
			loop.State = "waiting_approval"
			loop.UpdatedAt = time.Now().UTC()
			loop.Summary = loopSummary(loop)
			r.replaceAgentLoop(loop)
			return loop, nil
		}
		loop.State = "completed"
	}
	loop.UpdatedAt = time.Now().UTC()
	loop.Summary = loopSummary(loop)
	r.replaceAgentLoop(loop)
	return loop, nil
}

func (r *Runtime) hasTempAppSession(loop AgentLoop) bool {
	sessionID := strings.TrimSpace(loop.SessionID)
	if sessionID == "" {
		return false
	}
	session, ok := r.appSession(sessionID)
	if !ok {
		return false
	}
	return strings.TrimSpace(loop.WorkspaceRoot) != "" && session.Root == loop.WorkspaceRoot
}

func (r *Runtime) CancelAgentLoop(_ context.Context, id string) (AgentLoop, error) {
	loop, err := r.agentLoopByID(id)
	if err != nil {
		return AgentLoop{}, err
	}
	if loop.State == "completed" {
		return AgentLoop{}, errors.New("Agent loop is already completed.")
	}
	loop.State = "canceled"
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "cancel",
		Title:  "Cancel loop",
		State:  "completed",
		Detail: "Canceled by user.",
	})
	loop.UpdatedAt = time.Now().UTC()
	loop.Summary = loopSummary(loop)
	r.replaceAgentLoop(loop)
	return loop, nil
}

func (r *Runtime) agentLoopByID(id string) (AgentLoop, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return AgentLoop{}, errors.New("Agent loop ID is required.")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, loop := range r.agentLoops {
		if loop.ID == id {
			return loop, nil
		}
	}
	return AgentLoop{}, errors.New("Agent loop was not found.")
}

func (r *Runtime) replaceAgentLoop(loop AgentLoop) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, current := range r.agentLoops {
		if current.ID == loop.ID {
			r.agentLoops[index] = loop
			return
		}
	}
}

func (r *Runtime) runLoopSteps(ctx context.Context, loop AgentLoop, input AgentLoopInput) AgentLoop {
	goalLower := strings.ToLower(loop.Goal)
	if r.WorkspaceEnabled() {
		if shouldReadDiagnostics(goalLower) || shouldGatherAutoEvidence(goalLower, loop.Mode) {
			diagnostics, err := r.ListDiagnostics(ctx)
			loop = appendLoopStep(loop, "diagnostics", "Read diagnostics", "diagnostics", err, fmt.Sprintf("%d diagnostic(s)", len(diagnostics)), "")
			if loop.Mode == "auto" {
				loop = r.appendSubagentLoopStep(ctx, loop, "review", SubagentRunInput{Goal: loop.Goal})
			}
			if err == nil && len(diagnostics) > 0 && loop.Mode == "auto" {
				loop.Steps = append(loop.Steps, AgentLoopStep{
					ID:     newTraceID(),
					Kind:   "diagnostics_review",
					Title:  "Review diagnostics",
					State:  "attention",
					Detail: fmt.Sprintf("%d diagnostic(s) found.", len(diagnostics)),
					ToolID: "diagnostics",
				})
				loop.State = "attention"
				loop = r.autoProposeEdit(ctx, loop, EditPlanRequest{Goal: loop.Goal, Diagnostics: diagnostics})
				if loop.State == "waiting_approval" || loop.State == "waiting_input" || loop.State == "attention" {
					return loop
				}
			}
		}
		query := loopSearchQuery(input, loop.Goal)
		if query != "" {
			results, err := r.SearchFiles(ctx, query)
			loop = appendLoopStep(loop, "search_files", "Search workspace", "search_files", err, fmt.Sprintf("%d result(s) for %q", len(results), query), "")
			if loop.Mode == "auto" {
				subagentID := "search"
				if strings.Contains(goalLower, "doc") {
					subagentID = "docs"
				}
				loop = r.appendSubagentLoopStep(ctx, loop, subagentID, SubagentRunInput{Goal: loop.Goal, Query: query})
			}
		}
		filePath := strings.TrimSpace(input.FilePath)
		if filePath != "" {
			file, err := r.ReadFile(ctx, filePath)
			loop = appendLoopStep(loop, "read_file", "Read file", "read_file", err, fmt.Sprintf("%s · %d bytes", file.Path, file.Size), "")
		}
		if shouldReadSymbols(goalLower) {
			query := loopSymbolQuery(input, loop.Goal)
			symbols, err := r.ListSymbols(ctx, query)
			loop = appendLoopStep(loop, "symbols", "Read symbols", "symbols", err, fmt.Sprintf("%d symbol(s) for %q", len(symbols), query), "")
		}
		if shouldReadReferences(goalLower) {
			query := loopSymbolQuery(input, loop.Goal)
			references, err := r.ListReferences(ctx, query)
			loop = appendLoopStep(loop, "references", "Read references", "references", err, fmt.Sprintf("%d reference(s) for %q", len(references), query), "")
		}
	} else if shouldUseWorkspace(goalLower) || strings.TrimSpace(input.Query) != "" || strings.TrimSpace(input.FilePath) != "" {
		loop = appendLoopStep(loop, "workspace", "Use workspace tools", "workspace", ErrWorkspaceDisabled, "", "")
	}
	if strings.Contains(goalLower, "mcp") {
		servers := r.ListMCPServers(ctx)
		tools := r.ListMCPTools(ctx)
		resources := r.ListMCPResources(ctx)
		prompts := r.ListMCPPrompts(ctx)
		loop = appendLoopStep(loop, "mcp", "Inspect MCP", "mcp", nil, fmt.Sprintf("%d server(s), %d tool(s), %d resource(s), %d prompt(s)", len(servers), len(tools), len(resources), len(prompts)), "")
		if plan, ok := planMCPAction(goalLower, tools, resources, prompts); ok {
			loop = appendLoopStep(loop, "mcp_plan", plan.Title, plan.ToolID, nil, plan.Target, "")
			if plan.Kind == "mcp_call" {
				loop = appendLoopStep(loop, "mcp_boundary", "Review MCP tool", plan.ToolID, nil, "Run this MCP tool explicitly from System or TUI.", "")
				return loop
			}
			result := r.executeMCPPlanStep(ctx, plan)
			loop = appendLoopExecutionStep(loop, plan.Kind, plan.ExecutionTitle, plan.ToolID, result.Err, result.Detail, result.CreatedID)
			validation := validateMCPExecution(plan, result)
			loop = appendLoopStep(loop, "mcp_validate", "Validate MCP result", plan.ToolID, validation.Err, validation.Detail, "")
		}
	}
	if loop.Mode == "auto" && autoLoopLimitReached(loop) {
		return appendAutoLimitStep(loop)
	}
	proposalPath := strings.TrimSpace(input.ProposalPath)
	if proposalPath != "" {
		proposal, err := r.ProposeEdit(ctx, EditProposalInput{
			Path:    proposalPath,
			Content: input.ProposalContent,
			Summary: "Agent loop proposal",
		})
		detail := proposalPath
		createdID := ""
		if err == nil {
			detail = proposal.Path
			createdID = proposal.ID
		}
		loop = appendLoopStep(loop, "edit_proposal", "Create edit proposal", "edit_file", err, detail, "")
		if createdID != "" {
			loop.Steps[len(loop.Steps)-1].CreatedID = createdID
			loop.Steps = append(loop.Steps, AgentLoopStep{
				ID:        newTraceID(),
				Kind:      "edit_review",
				Title:     "Review edit proposal",
				State:     "waiting_approval",
				Detail:    "Review and apply explicitly before running more checks.",
				ToolID:    "edit_file",
				CreatedID: createdID,
			})
			loop.State = "waiting_approval"
		}
		return loop
	} else if shouldRequestEditBoundary(goalLower) && !hasCompletedEditReview(loop) {
		if loop.Mode == "auto" && r.editPlanner != nil {
			files, err := r.autoCreateContextFiles(ctx, loop.Goal)
			if err != nil {
				loop = appendLoopStep(loop, "read_file", "Read create context", "read_file", err, "", "")
				return loop
			}
			loop = r.autoProposeEdit(ctx, loop, EditPlanRequest{
				Goal:  loop.Goal,
				Files: files,
			})
			if loop.State == "waiting_approval" || loop.State == "waiting_input" || loop.State == "attention" {
				return loop
			}
			if hasCompletedEditReview(loop) {
				if loop.Mode == "auto" && autoLoopLimitReached(loop) {
					return appendAutoLimitStep(loop)
				}
				goto afterEditBoundary
			}
		}
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "edit_boundary",
			Title:  "Edit boundary",
			State:  "waiting_approval",
			Detail: "Provide proposal path and content before creating an edit proposal.",
			ToolID: "edit_file",
		})
		if loop.State != "waiting_input" {
			loop.State = "waiting_approval"
		}
		return loop
	}
afterEditBoundary:
	command := strings.Join(strings.Fields(input.Command), " ")
	if command == "" && loop.Mode == "auto" && mentionsCommand(goalLower) {
		inferred, detail, projectCheck := r.inferLoopCommand(ctx, loop.Goal)
		if inferred != "" {
			command = inferred
			loop = appendLoopStep(loop, "command_infer", "Choose check command", "run_command", nil, detail, command)
			if projectCheck {
				loop = r.runInferredProjectCommand(ctx, loop, command)
				return loop
			}
		}
	}
	if command != "" {
		check, err := r.CheckCommand(ctx, CommandCheckInput{Command: command})
		detail := "blocked"
		if err == nil {
			detail = check.Reason
		}
		if err == nil && !check.Allowed {
			err = errors.New("command is not in allowlist")
		}
		loop = appendLoopStep(loop, "command_check", "Check command", "run_command", err, detail, command)
		if err == nil && check.Allowed {
			if loop.Mode == "auto" {
				approval, approvalErr := r.AddCommandApproval(ctx, CommandApprovalInput{
					Command: command,
					State:   "approved",
					Detail:  "Auto loop approved allowlisted command.",
				})
				if approvalErr != nil {
					loop = appendLoopStep(loop, "command_approval", "Approve command", "run_command", approvalErr, "", command)
				} else {
					loop.Steps = append(loop.Steps, AgentLoopStep{
						ID:        newTraceID(),
						Kind:      "command_approval",
						Title:     "Approve command",
						State:     "completed",
						Detail:    "Auto loop approved allowlisted command.",
						ToolID:    "run_command",
						Command:   command,
						CreatedID: approval.ID,
					})
					loop = r.runLoopCommand(ctx, loop, command, approval.ID)
				}
				return loop
			}
			approval, approvalErr := r.AddCommandApproval(ctx, CommandApprovalInput{
				Command: command,
				State:   "pending",
				Detail:  "Agent loop requested command approval.",
			})
			if approvalErr != nil {
				loop = appendLoopStep(loop, "command_approval", "Request command approval", "run_command", approvalErr, "", command)
			} else {
				step := AgentLoopStep{
					ID:        newTraceID(),
					Kind:      "command_approval",
					Title:     "Request command approval",
					State:     "waiting_approval",
					Detail:    "Approve before running.",
					ToolID:    "run_command",
					Command:   command,
					CreatedID: approval.ID,
				}
				loop.Steps = append(loop.Steps, step)
				loop.State = "waiting_approval"
			}
		}
	} else if mentionsCommand(goalLower) {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "command_approval",
			Title:  "Need command",
			State:  "waiting_input",
			Detail: "Choose an allowlisted command to check.",
			ToolID: "run_command",
		})
		loop.State = "waiting_input"
	}
	return loop
}

func (r *Runtime) consumeAppliedEditReviews(loop AgentLoop) (AgentLoop, bool) {
	blocked := false
	for index, step := range loop.Steps {
		if step.Kind != "edit_review" || step.State != "waiting_approval" || strings.TrimSpace(step.CreatedID) == "" {
			continue
		}
		proposal, ok := r.editProposalByID(step.CreatedID)
		if !ok {
			loop.Steps[index].State = "blocked"
			loop.Steps[index].Detail = "Edit proposal was not found."
			loop.State = "attention"
			blocked = true
			continue
		}
		switch proposal.Status {
		case "applied":
			loop.Steps[index].State = "completed"
			loop.Steps[index].Detail = "Proposal applied."
		case "rejected":
			loop.Steps[index].State = "rejected"
			loop.Steps[index].Detail = "Proposal rejected."
			loop = appendRetryStep(loop, loopRetryDetail(loop, "Proposal rejected."))
			blocked = true
		default:
			loop.State = "waiting_approval"
			blocked = true
		}
	}
	return loop, blocked
}

func (r *Runtime) runLoopCommand(ctx context.Context, loop AgentLoop, command string, approvalID string) AgentLoop {
	run, runErr := r.RunCommand(ctx, CommandCheckInput{Command: command, ApprovalID: approvalID})
	detail := fmt.Sprintf("exit %d", run.ExitCode)
	if runErr == nil && run.ExitCode != 0 {
		runErr = fmt.Errorf("command exited with %d", run.ExitCode)
	}
	if runErr != nil {
		detail = runErr.Error()
	}
	loop = appendLoopStep(loop, "command_run", "Run command", "run_command", runErr, detail, command)
	goalLower := strings.ToLower(loop.Goal)
	if runErr != nil {
		if loop.Mode == "auto" && r.WorkspaceEnabled() && shouldReadDiagnostics(goalLower) {
			diagnostics, err := r.ListDiagnostics(ctx)
			loop = appendLoopStep(loop, "diagnostics", "Read diagnostics", "diagnostics", err, fmt.Sprintf("%d diagnostic(s) after failed command", len(diagnostics)), "")
			if err == nil && len(diagnostics) > 0 {
				loop.Steps = append(loop.Steps, AgentLoopStep{
					ID:     newTraceID(),
					Kind:   "diagnostics_review",
					Title:  "Review diagnostics",
					State:  "attention",
					Detail: fmt.Sprintf("%d diagnostic(s) remain.", len(diagnostics)),
					ToolID: "diagnostics",
				})
				loop.State = "attention"
				loop = r.autoProposeEdit(ctx, loop, EditPlanRequest{
					Goal:          loop.Goal,
					Diagnostics:   diagnostics,
					Command:       command,
					CommandOutput: strings.TrimSpace(run.Output),
				})
			} else {
				loop = appendRetryStep(loop, loopRetryDetail(loop, "Command failed."))
			}
		} else {
			loop = appendRetryStep(loop, loopRetryDetail(loop, "Command failed."))
		}
		return loop
	}
	loop = appendLoopStep(loop, "review_result", "Review result", "run_command", nil, "Command completed successfully.", command)
	if r.WorkspaceEnabled() && shouldReadDiagnostics(goalLower) {
		diagnostics, err := r.ListDiagnostics(ctx)
		loop = appendLoopStep(loop, "diagnostics", "Read diagnostics", "diagnostics", err, fmt.Sprintf("%d diagnostic(s) after command", len(diagnostics)), "")
		if err == nil && len(diagnostics) > 0 {
			loop.Steps = append(loop.Steps, AgentLoopStep{
				ID:     newTraceID(),
				Kind:   "diagnostics_review",
				Title:  "Review diagnostics",
				State:  "attention",
				Detail: fmt.Sprintf("%d diagnostic(s) remain.", len(diagnostics)),
				ToolID: "diagnostics",
			})
			loop.State = "attention"
			if loop.Mode == "auto" {
				loop = r.autoProposeEdit(ctx, loop, EditPlanRequest{
					Goal:          loop.Goal,
					Diagnostics:   diagnostics,
					Command:       command,
					CommandOutput: strings.TrimSpace(run.Output),
				})
			} else {
				loop = appendRetryStep(loop, loopRetryDetail(loop, "Diagnostics remain."))
			}
		}
	}
	return loop
}

func appendLoopStep(loop AgentLoop, kind string, title string, toolID string, err error, detail string, command string) AgentLoop {
	state := "completed"
	if err != nil {
		state = "blocked"
		detail = err.Error()
		if errors.Is(err, ErrWorkspaceDisabled) {
			state = "waiting_input"
		}
	}
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:      newTraceID(),
		Kind:    kind,
		Title:   title,
		State:   state,
		Detail:  detail,
		ToolID:  toolID,
		Command: command,
	})
	if state == "blocked" {
		loop.State = "attention"
	}
	if state == "waiting_input" && loop.State != "attention" {
		loop.State = "waiting_input"
	}
	return loop
}

func appendLoopExecutionStep(loop AgentLoop, kind string, title string, toolID string, err error, detail string, createdID string) AgentLoop {
	loop = appendLoopStep(loop, kind, title, toolID, err, detail, "")
	if strings.TrimSpace(createdID) != "" && len(loop.Steps) > 0 {
		loop.Steps[len(loop.Steps)-1].CreatedID = createdID
	}
	return loop
}

func (r *Runtime) appendSubagentLoopStep(ctx context.Context, loop AgentLoop, subagentID string, input SubagentRunInput) AgentLoop {
	run, err := r.RunSubagent(ctx, subagentID, input)
	detail := run.Summary
	state := run.State
	createdID := run.ID
	if err != nil {
		detail = err.Error()
		state = "blocked"
	}
	if strings.TrimSpace(detail) == "" {
		detail = subagentID
	}
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:        newTraceID(),
		Kind:      "subagent_run",
		Title:     "Run subagent",
		State:     state,
		Detail:    fmt.Sprintf("%s: %s", subagentID, detail),
		ToolID:    "subagent",
		CreatedID: createdID,
	})
	if state == "blocked" {
		loop.State = "attention"
	}
	return loop
}

func appendRetryStep(loop AgentLoop, detail string) AgentLoop {
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "retry",
		Title:  "Retry",
		State:  "waiting_input",
		Detail: detail,
	})
	loop.State = "attention"
	return loop
}

func appendAutoLimitStep(loop AgentLoop) AgentLoop {
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:     newTraceID(),
		Kind:   "auto_limit",
		Title:  "Auto limit",
		State:  "waiting_input",
		Detail: fmt.Sprintf("Reached %d iteration(s). Continue explicitly to keep going.", loop.MaxIterations),
	})
	loop.State = "waiting_input"
	return loop
}

func (r *Runtime) runInferredProjectCommand(ctx context.Context, loop AgentLoop, command string) AgentLoop {
	check := CommandCheck{
		ID:        newTraceID(),
		Command:   command,
		Allowed:   true,
		Reason:    "inferred project check",
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	r.commandChecks = append([]CommandCheck{check}, r.commandChecks...)
	if len(r.commandChecks) > 50 {
		r.commandChecks = r.commandChecks[:50]
	}
	r.mu.Unlock()
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:      newTraceID(),
		Kind:    "command_check",
		Title:   "Check command",
		State:   "completed",
		Detail:  check.Reason,
		ToolID:  "run_command",
		Command: command,
	})
	approval := CommandApproval{
		ID:        newTraceID(),
		Command:   command,
		State:     "approved",
		Detail:    "Auto loop approved inferred project check.",
		CreatedAt: time.Now().UTC(),
	}
	r.mu.Lock()
	r.commandApprovals = append([]CommandApproval{approval}, r.commandApprovals...)
	if len(r.commandApprovals) > 50 {
		r.commandApprovals = r.commandApprovals[:50]
	}
	r.mu.Unlock()
	loop.Steps = append(loop.Steps, AgentLoopStep{
		ID:        newTraceID(),
		Kind:      "command_approval",
		Title:     "Approve command",
		State:     "completed",
		Detail:    "Auto loop approved inferred project check.",
		ToolID:    "run_command",
		Command:   command,
		CreatedID: approval.ID,
	})
	run, err := r.runCheckedCommand(ctx, command)
	detail := fmt.Sprintf("exit %d", run.ExitCode)
	if err == nil && run.ExitCode != 0 {
		err = fmt.Errorf("command exited with %d", run.ExitCode)
	}
	if err != nil {
		detail = err.Error()
	}
	loop = appendLoopStep(loop, "command_run", "Run command", "run_command", err, detail, command)
	return loop
}

func (r *Runtime) inferLoopCommand(ctx context.Context, goal string) (string, string, bool) {
	if strings.TrimSpace(goal) == "" {
		return "", "", false
	}
	root, err := r.workspaceRootPath()
	if err != nil {
		return "", "", false
	}
	goalLower := strings.ToLower(goal)
	if command, detail := r.inferPackageCommand(ctx, root, goalLower); command != "" {
		return command, detail, true
	}
	candidates := []struct {
		command string
		file    string
		terms   []string
	}{
		{command: "make test", file: "Makefile", terms: []string{"test", "check", "fix", "build"}},
		{command: "make check", file: "Makefile", terms: []string{"check", "fix", "build"}},
		{command: "make ui-check-agent", file: "Makefile", terms: []string{"ui", "agent", "frontend"}},
		{command: "make tui-check", file: "Makefile", terms: []string{"tui", "terminal"}},
		{command: "go test ./...", file: "go.mod", terms: []string{"test", "check", "fix"}},
		{command: "go vet ./...", file: "go.mod", terms: []string{"vet", "check"}},
		{command: "npm run build", file: "package.json", terms: []string{"build", "frontend", "ui", "typescript"}},
	}
	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			return "", "", false
		default:
		}
		if !goalMatchesCommandTerms(goalLower, candidate.terms) {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, candidate.file)); err == nil {
			return candidate.command, fmt.Sprintf("Inferred from %s and goal.", candidate.file), true
		}
	}
	for _, command := range r.allowedCommands() {
		if goalMatchesCommand(command, goalLower) {
			return command, "Inferred from command allowlist and goal.", false
		}
	}
	return "", "", false
}

func (r *Runtime) inferPackageCommand(ctx context.Context, root string, goal string) (string, string) {
	select {
	case <-ctx.Done():
		return "", ""
	default:
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return "", ""
	}
	var packageFile struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &packageFile); err != nil || len(packageFile.Scripts) == 0 {
		return "", ""
	}
	for _, script := range packageScriptPreference(goal) {
		if strings.TrimSpace(packageFile.Scripts[script]) == "" {
			continue
		}
		for _, command := range packageScriptCommands(script) {
			return command, fmt.Sprintf("Inferred from package.json script %q.", script)
		}
	}
	return "", ""
}

func packageScriptPreference(goal string) []string {
	switch {
	case strings.Contains(goal, "lint"):
		return []string{"lint", "check", "test", "build"}
	case strings.Contains(goal, "type") || strings.Contains(goal, "typescript"):
		return []string{"typecheck", "type-check", "check", "build", "test"}
	case strings.Contains(goal, "test"):
		return []string{"test", "check", "lint", "build"}
	case strings.Contains(goal, "build"):
		return []string{"build", "check", "test", "lint"}
	case strings.Contains(goal, "ui") || strings.Contains(goal, "frontend") || strings.Contains(goal, "react"):
		return []string{"build", "test", "lint", "check"}
	default:
		return []string{"check", "test", "build", "lint"}
	}
}

func packageScriptCommands(script string) []string {
	if script == "test" {
		return []string{"npm test", "npm run test"}
	}
	return []string{fmt.Sprintf("npm run %s", script)}
}

func (r *Runtime) autoProposeEdit(ctx context.Context, loop AgentLoop, request EditPlanRequest) AgentLoop {
	if autoLoopLimitReached(loop) {
		return appendAutoLimitStep(loop)
	}
	planner := r.editPlanner
	if planner == nil {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "plan_edit",
			Title:  "Plan edit",
			State:  "waiting_input",
			Detail: "Auto edit planning is unavailable.",
			ToolID: "edit_file",
		})
		loop.State = "waiting_input"
		return loop
	}
	files := request.Files
	if len(files) == 0 {
		var err error
		files, err = r.autoEditContextFiles(ctx, request.Diagnostics)
		if err != nil {
			loop = appendLoopStep(loop, "read_file", "Read fix context", "read_file", err, "", "")
			return loop
		}
	}
	if len(files) == 0 {
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:     newTraceID(),
			Kind:   "plan_edit",
			Title:  "Plan edit",
			State:  "waiting_input",
			Detail: "No editable diagnostic file was found.",
			ToolID: "edit_file",
		})
		loop.State = "waiting_input"
		return loop
	}
	request.Files = files
	plan, err := planner.PlanEdit(ctx, request)
	if err != nil {
		fallback, ok := autoCreateFallbackPlan(request, files)
		if !ok {
			loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", err, "", "")
			return loop
		}
		plan = fallback
	}
	plan.Path = strings.TrimSpace(plan.Path)
	if plan.Path == "" {
		fallback, ok := autoCreateFallbackPlan(request, files)
		if !ok {
			loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", errors.New("planner returned no path"), "", "")
			return loop
		}
		plan = fallback
	}
	if !editPlanPathInFiles(plan.Path, files) {
		fallback, ok := autoCreateFallbackPlan(request, files)
		if !ok {
			loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", errors.New("planner returned a path outside diagnostic context"), "", "")
			return loop
		}
		plan = fallback
	}
	loop = appendLoopStep(loop, "plan_edit", "Plan edit", "edit_file", nil, plan.Path, "")
	proposal, err := r.ProposeEdit(ctx, EditProposalInput{
		Path:    plan.Path,
		Content: plan.Content,
		Summary: firstNonEmpty(strings.TrimSpace(plan.Summary), "Auto loop proposal"),
	})
	detail := plan.Path
	createdID := ""
	if err == nil {
		detail = proposal.Path
		createdID = proposal.ID
	}
	loop = appendLoopStep(loop, "edit_proposal", "Create edit proposal", "edit_file", err, detail, "")
	if createdID != "" {
		loop.Steps[len(loop.Steps)-1].CreatedID = createdID
		if loop.Mode == "auto" && loop.AutoApply {
			return r.autoApplyGeneratedEditProposal(ctx, loop, createdID)
		}
		loop.Steps = append(loop.Steps, AgentLoopStep{
			ID:        newTraceID(),
			Kind:      "edit_review",
			Title:     "Review edit proposal",
			State:     "waiting_approval",
			Detail:    "Review and apply explicitly before running more checks.",
			ToolID:    "edit_file",
			CreatedID: createdID,
		})
		loop.State = "waiting_approval"
	}
	return loop
}

func (r *Runtime) autoApplyGeneratedEditProposal(ctx context.Context, loop AgentLoop, proposalID string) AgentLoop {
	proposal, err := r.ReviewEditProposal(ctx, proposalID, EditProposalReviewInput{
		Status: "approved",
		Detail: "Approved by auto loop.",
	})
	if err == nil {
		proposal, err = r.ApplyEditProposal(ctx, proposalID)
	}
	step := AgentLoopStep{
		ID:        newTraceID(),
		Kind:      "edit_review",
		Title:     "Apply edit proposal",
		ToolID:    "edit_file",
		CreatedID: proposalID,
	}
	if err != nil {
		step.State = "blocked"
		step.Detail = err.Error()
		loop.State = "attention"
	} else {
		step.State = "completed"
		step.Detail = "Proposal applied."
		loop.State = "running"
		if proposal.Path != "" {
			step.Detail = "Applied " + proposal.Path + "."
		}
	}
	loop.Steps = append(loop.Steps, step)
	return loop
}

func (r *Runtime) autoEditContextFiles(ctx context.Context, diagnostics []Diagnostic) ([]FileResult, error) {
	seen := map[string]bool{}
	files := []FileResult{}
	for _, diagnostic := range diagnostics {
		path := strings.TrimSpace(diagnostic.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		file, err := r.ReadFile(ctx, path)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
		if len(files) >= 3 {
			break
		}
	}
	return files, nil
}

func (r *Runtime) autoCreateContextFiles(ctx context.Context, goal string) ([]FileResult, error) {
	path := autoCreateProposalPath(goal)
	file, err := r.ReadFile(ctx, path)
	if err == nil {
		return []FileResult{file}, nil
	}
	if os.IsNotExist(err) {
		return []FileResult{{
			Path:    path,
			Content: "",
			Size:    0,
		}}, nil
	}
	return nil, err
}

func autoCreateProposalPath(goal string) string {
	goal = strings.ToLower(goal)
	switch {
	case strings.Contains(goal, "portfolio"):
		return "portfolio.html"
	case strings.Contains(goal, "react"):
		return "react-page.html"
	case strings.Contains(goal, "readme"):
		return "README.md"
	case strings.Contains(goal, "doc"):
		return "agent-notes.md"
	default:
		return "agent-output.md"
	}
}

func shouldUseTempAppWorkspace(goal string) bool {
	goal = strings.ToLower(goal)
	if !(strings.Contains(goal, "react") && strings.Contains(goal, "app")) {
		return false
	}
	if strings.Contains(goal, "propose") ||
		strings.Contains(goal, "file change") ||
		strings.Contains(goal, "edit proposal") ||
		strings.Contains(goal, "build check") {
		return false
	}
	if mentionsCurrentWorkspaceTarget(goal) {
		return false
	}
	return strings.Contains(goal, "create") ||
		strings.Contains(goal, "creat") ||
		strings.Contains(goal, "build") ||
		strings.Contains(goal, "make") ||
		strings.Contains(goal, "generate")
}

func shouldStartFreshTempAppWorkspace(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	if !(strings.Contains(goal, "react") && strings.Contains(goal, "app")) {
		return false
	}
	if strings.HasPrefix(goal, "edit ") ||
		strings.HasPrefix(goal, "change ") ||
		strings.HasPrefix(goal, "update ") ||
		strings.Contains(goal, "make it") ||
		strings.Contains(goal, "turn it") {
		return false
	}
	return strings.Contains(goal, "create") ||
		strings.Contains(goal, "creat") ||
		strings.Contains(goal, "generate") ||
		strings.Contains(goal, "build a") ||
		strings.Contains(goal, "build an") ||
		strings.Contains(goal, "make a") ||
		strings.Contains(goal, "make an")
}

func mentionsCurrentWorkspaceTarget(goal string) bool {
	return strings.Contains(goal, "current repo") ||
		strings.Contains(goal, "this repo") ||
		strings.Contains(goal, "current repository") ||
		strings.Contains(goal, "this repository") ||
		strings.Contains(goal, "current workspace") ||
		strings.Contains(goal, "this workspace") ||
		strings.Contains(goal, "current project") ||
		strings.Contains(goal, "this project") ||
		strings.Contains(goal, "codebase")
}

func tempAppPackageJSON() string {
	return `{
  "name": "linea-temp-app",
  "private": true,
  "type": "module"
}
`
}

func tempAppIndexHTML() string {
	return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Linea temp app</title>
    <link rel="stylesheet" href="./assets/app.css">
  </head>
  <body>
    <main id="root"></main>
    <script type="module" src="./assets/app.js"></script>
  </body>
</html>
`
}

func tempAppMainJSX() string {
	return `import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App.jsx";

createRoot(document.getElementById("root")).render(React.createElement(App));
`
}

func tempAppReactShim() string {
	return `export const Fragment = Symbol("Fragment");

let rerender = () => {};
let hookValues = [];
let hookIndex = 0;

export function __beginRender() {
  hookIndex = 0;
}

export function __setRerender(next) {
  rerender = next;
}

export function useState(initialValue) {
  const index = hookIndex++;
  if (hookValues[index] === undefined) {
    hookValues[index] = initialValue;
  }
  const setValue = (nextValue) => {
    hookValues[index] = typeof nextValue === "function" ? nextValue(hookValues[index]) : nextValue;
    rerender();
  };
  return [hookValues[index], setValue];
}

export function useEffect(callback) {
  if (typeof callback === "function") {
    queueMicrotask(callback);
  }
}

export function useMemo(callback) {
  return typeof callback === "function" ? callback() : callback;
}

export function useRef(initialValue) {
  return { current: initialValue };
}

export function createElement(type, props, ...children) {
  return { type, props: props || {}, children: children.flat() };
}

export function __renderNode(node) {
  if (node === null || node === undefined || node === false) {
    return document.createTextNode("");
  }
  if (Array.isArray(node)) {
    const fragment = document.createDocumentFragment();
    node.forEach((child) => fragment.appendChild(__renderNode(child)));
    return fragment;
  }
  if (typeof node === "string" || typeof node === "number") {
    return document.createTextNode(String(node));
  }
  if (node.type === Fragment) {
    return __renderNode(node.children);
  }
  if (typeof node.type === "function") {
    return __renderNode(node.type({ ...node.props, children: node.children }));
  }
  const element = document.createElement(node.type);
  for (const [key, value] of Object.entries(node.props || {})) {
    if (key === "children" || value === undefined || value === null) {
      continue;
    }
    if (key === "style" && typeof value === "object") {
      Object.assign(element.style, value);
      continue;
    }
    if (key.startsWith("on") && typeof value === "function") {
      element.addEventListener(key.slice(2).toLowerCase(), value);
      continue;
    }
    element.setAttribute(key === "className" ? "class" : key, String(value));
  }
  node.children.forEach((child) => element.appendChild(__renderNode(child)));
  return element;
}

export default { createElement, Fragment, useEffect, useMemo, useRef, useState };
`
}

func tempAppReactDOMShim() string {
	return `import { __beginRender, __renderNode, __setRerender } from "./react.js";

export function createRoot(container) {
  let current = null;
  const draw = () => {
    __beginRender();
    container.replaceChildren(__renderNode(current));
  };
  __setRerender(draw);
  return {
    render(node) {
      current = node;
      draw();
    }
  };
}
`
}

func tempMessageAppJSX(message string) string {
	title, _ := json.Marshal(strings.TrimSpace(message))
	return fmt.Sprintf(`import React from "react";

export default function App() {
  return React.createElement("h1", null, %s);
}
`, string(title))
}

func tempDefaultAppJSX() string {
	return `import React, { useState } from "react";

export default function App() {
  const [focus, setFocus] = useState("work");
  const items = {
    work: [
      ["Linea", "Local-first chat, tool boundaries, and inspectable agent loops."],
      ["Preview", "Temporary app generation with a clean browser preview."],
      ["Systems", "Practical interfaces that stay understandable as they grow."]
    ],
    process: [
      ["Plan", "Break product requests into visible steps."],
      ["Build", "Write package files inside a temp workspace."],
      ["Check", "Run package scripts before presenting the preview."]
    ]
  };

  return React.createElement("main", { style: styles.shell },
    React.createElement("h1", { style: styles.title }, "React app"),
    React.createElement("p", { style: styles.copy }, "Generated in a temporary Linea package. The main codebase is untouched."),
    React.createElement("div", { style: styles.actions },
      Object.keys(items).map((item) =>
        React.createElement("button", {
          key: item,
          onClick: () => setFocus(item),
          style: focus === item ? { ...styles.button, ...styles.active } : styles.button
        }, item)
      )
    ),
    React.createElement("section", { style: styles.grid },
      items[focus].map(([heading, body]) =>
        React.createElement("article", { key: heading, style: styles.card },
          React.createElement("h2", null, heading),
          React.createElement("p", { style: styles.copy }, body)
        )
      )
    )
  );
}

const styles = {
  shell: {
    minHeight: "100vh",
    margin: 0,
    padding: "72px min(7vw, 72px)",
    background: "#101215",
    color: "#f6f3ed",
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
  },
  title: { maxWidth: 760, margin: 0, fontSize: "clamp(44px, 7vw, 78px)", lineHeight: 0.96 },
  copy: { color: "#cfc7bc", fontSize: 18, lineHeight: 1.6 },
  actions: { display: "flex", gap: 12, flexWrap: "wrap", marginTop: 28 },
  button: { border: "1px solid rgba(246,243,237,.16)", borderRadius: 999, background: "rgba(255,255,255,.08)", color: "inherit", padding: "11px 16px" },
  active: { background: "#f6f3ed", color: "#101215" },
  grid: { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 16, marginTop: 42 },
  card: { border: "1px solid rgba(246,243,237,.13)", borderRadius: 16, padding: 22, background: "rgba(255,255,255,.05)" }
};
`
}

func tempLouderAppJSX() string {
	return `import React from "react";

export default function App() {
  return React.createElement("main", { style: {
    minHeight: "100vh",
    display: "grid",
    placeItems: "center",
    background: "linear-gradient(135deg, #101215, #224b49)",
    color: "#f6f3ed",
    fontFamily: "Inter, system-ui, sans-serif"
  } }, React.createElement("h1", { style: {
    fontSize: "clamp(72px, 16vw, 180px)",
    letterSpacing: 0,
    margin: 0
  } }, "Hi"));
}
`
}

func tempColorAppJSX(message string, color string) string {
	title, _ := json.Marshal(firstNonEmpty(strings.TrimSpace(message), "Hi"))
	background, foreground := tempAppColorPair(color)
	return fmt.Sprintf(`import React from "react";

export default function App() {
  return React.createElement("main", { style: {
    minHeight: "100vh",
    display: "grid",
    placeItems: "center",
    background: %q,
    color: %q,
    fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif"
  } }, React.createElement("h1", null, %s));
}
`, background, foreground, string(title))
}

func tempAppColorGoal(goal string) (string, bool) {
	goal = strings.ToLower(goal)
	for _, color := range []string{"blue", "green", "red", "purple", "pink", "yellow", "orange", "black", "white"} {
		if strings.Contains(goal, color) {
			return color, true
		}
	}
	return "", false
}

func tempAppColorPair(color string) (string, string) {
	switch color {
	case "blue":
		return "#1d4ed8", "#f8fafc"
	case "green":
		return "#166534", "#f7fee7"
	case "red":
		return "#991b1b", "#fff1f2"
	case "purple":
		return "#581c87", "#faf5ff"
	case "pink":
		return "#9d174d", "#fdf2f8"
	case "yellow":
		return "#facc15", "#111827"
	case "orange":
		return "#c2410c", "#fff7ed"
	case "white":
		return "#f8fafc", "#111827"
	default:
		return "#111827", "#f8fafc"
	}
}

func tempAppDisplayText(source string) string {
	if match := jsxTextPattern.FindStringSubmatch(source); len(match) > 1 {
		return trimRunes(strings.Join(strings.Fields(match[1]), " "), 100)
	}
	if match := createElementTextPattern.FindStringSubmatch(source); len(match) > 1 {
		return trimRunes(strings.Join(strings.Fields(match[1]), " "), 100)
	}
	return "Hi"
}

func shouldMakeAppLouder(goal string) bool {
	goal = strings.ToLower(goal)
	return strings.Contains(goal, "bigger") ||
		strings.Contains(goal, "larger") ||
		strings.Contains(goal, "louder") ||
		strings.Contains(goal, "more visible")
}

func tempAppMessage(goal string) (string, bool) {
	trimmed := strings.TrimSpace(goal)
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{"says only ", "say only ", "says ", "say "} {
		index := strings.Index(lower, marker)
		if index < 0 {
			continue
		}
		message := strings.Trim(trimmed[index+len(marker):], " .!\"'")
		if message == "" {
			continue
		}
		return trimRunes(message, 80), true
	}
	return "", false
}

func isGenericTempAppGoal(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))
	return goal == "create a react app" ||
		goal == "create react app" ||
		goal == "build a react app" ||
		goal == "make a react app" ||
		goal == "generate a react app"
}

func autoCreateFallbackPlan(request EditPlanRequest, files []FileResult) (EditPlan, bool) {
	if len(request.Diagnostics) != 0 || len(files) != 1 || strings.TrimSpace(files[0].Content) != "" || files[0].Size != 0 {
		return EditPlan{}, false
	}
	path := strings.TrimSpace(files[0].Path)
	switch path {
	case "portfolio.html", "react-page.html":
	default:
		return EditPlan{}, false
	}
	return EditPlan{
		Path:    path,
		Content: defaultReactPortfolioHTML(),
		Summary: "Create React portfolio",
	}, true
}

func defaultReactPortfolioHTML() string {
	return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>React Portfolio</title>
    <style>
      :root {
        color-scheme: light dark;
        font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        background: #101215;
        color: #f6f3ed;
      }
      body {
        margin: 0;
        min-height: 100vh;
        background: radial-gradient(circle at top left, rgba(87, 115, 115, 0.28), transparent 34%), #101215;
      }
      main {
        width: min(920px, calc(100vw - 48px));
        margin: 0 auto;
        padding: 72px 0;
      }
      h1 {
        margin: 0 0 16px;
        font-size: clamp(42px, 7vw, 76px);
        line-height: 0.95;
      }
      p {
        color: #c9c2b8;
        font-size: 18px;
        line-height: 1.6;
      }
      .work {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
        gap: 16px;
        margin-top: 36px;
      }
      article {
        border: 1px solid rgba(246, 243, 237, 0.14);
        border-radius: 14px;
        padding: 20px;
        background: rgba(255, 255, 255, 0.04);
      }
      h2 {
        margin: 0 0 8px;
        font-size: 18px;
      }
    </style>
  </head>
  <body>
    <main>
      <h1>Product engineer portfolio</h1>
      <p>I build local-first tools with clear interfaces, practical automation, and careful review boundaries.</p>
      <section class="work">
        <article>
          <h2>Linea</h2>
          <p>Local-first AI assistant with chat, tools, and bounded agent loops.</p>
        </article>
        <article>
          <h2>Studio</h2>
          <p>A compact interface system for calm, repeatable product work.</p>
        </article>
        <article>
          <h2>Notes</h2>
          <p>Small workflow utilities for research, drafts, and checks.</p>
        </article>
      </section>
    </main>
  </body>
</html>
`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func hasExplicitLoopContinueInput(input AgentLoopContinueInput) bool {
	return strings.TrimSpace(input.Command) != "" ||
		strings.TrimSpace(input.Query) != "" ||
		strings.TrimSpace(input.FilePath) != "" ||
		strings.TrimSpace(input.ProposalPath) != ""
}

func editPlanPathInFiles(path string, files []FileResult) bool {
	path = strings.Trim(strings.TrimSpace(path), "/")
	for _, file := range files {
		if strings.Trim(strings.TrimSpace(file.Path), "/") == path {
			return true
		}
	}
	return false
}

func normalizeAgentLoopMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		return "auto"
	default:
		return "guided"
	}
}

func normalizeAgentLoopIterations(mode string, limit int) int {
	if mode != "auto" {
		return 0
	}
	if limit <= 0 {
		return defaultAutoLoopIterations
	}
	if limit > maxAutoLoopIterationsLimit {
		return maxAutoLoopIterationsLimit
	}
	return limit
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func loopPlanDetail(mode string) string {
	if mode == "auto" {
		return "Created an auto local plan with bounded local steps."
	}
	return "Created a bounded local plan."
}

func loopRetryDetail(loop AgentLoop, reason string) string {
	if loop.Mode == "auto" {
		return reason + " Auto loop paused for the next proposal or approved command."
	}
	return reason + " Provide another proposal or command to continue."
}

func autoLoopLimitReached(loop AgentLoop) bool {
	if loop.Mode != "auto" || loop.MaxIterations <= 0 {
		return false
	}
	iterations := 0
	for _, step := range loop.Steps {
		switch step.Kind {
		case "command_run", "edit_proposal":
			iterations++
		}
	}
	return iterations >= loop.MaxIterations
}

func hasUnresolvedEditBoundary(loop AgentLoop) bool {
	return hasUnresolvedEditBoundaryInSteps(loop.Steps)
}

func hasUnresolvedEditReview(loop AgentLoop) bool {
	for index := len(loop.Steps) - 1; index >= 0; index-- {
		switch loop.Steps[index].Kind {
		case "edit_review":
			return loop.Steps[index].State == "waiting_approval"
		case "command_approval", "command_run", "command_retry":
			return false
		}
	}
	return false
}

func hasCompletedEditReview(loop AgentLoop) bool {
	for _, step := range loop.Steps {
		if step.Kind == "edit_review" && step.State == "completed" {
			return true
		}
	}
	return false
}

func hasUnresolvedEditBoundaryBefore(loop AgentLoop, index int) bool {
	if index < 0 {
		return false
	}
	if index > len(loop.Steps) {
		index = len(loop.Steps)
	}
	return hasUnresolvedEditBoundaryInSteps(loop.Steps[:index])
}

func hasUnresolvedEditBoundaryInSteps(steps []AgentLoopStep) bool {
	for index := len(steps) - 1; index >= 0; index-- {
		switch steps[index].Kind {
		case "edit_review":
			return steps[index].State != "completed"
		case "edit_proposal":
			return false
		case "edit_boundary":
			return steps[index].State == "waiting_approval" || steps[index].State == "waiting_input"
		}
	}
	return false
}

func loopSearchQuery(input AgentLoopInput, goal string) string {
	query := strings.TrimSpace(input.Query)
	if query != "" {
		return query
	}
	lower := strings.ToLower(goal)
	for _, prefix := range []string{"search ", "find ", "look for "} {
		index := strings.Index(lower, prefix)
		if index >= 0 {
			term := strings.TrimSpace(goal[index+len(prefix):])
			if len([]rune(term)) > 1 {
				return trimRunes(term, 80)
			}
		}
	}
	return ""
}

func loopSymbolQuery(input AgentLoopInput, goal string) string {
	query := strings.TrimSpace(input.Query)
	if query != "" {
		return query
	}
	goal = strings.TrimSpace(goal)
	lower := strings.ToLower(goal)
	for _, prefix := range []string{
		"find definition ",
		"find reference ",
		"find references ",
		"definition ",
		"reference ",
		"references ",
		"navigate ",
		"symbol ",
		"symbols ",
	} {
		if index := strings.Index(lower, prefix); index >= 0 {
			return trimRunes(trimSymbolQueryTerm(goal[index+len(prefix):]), 80)
		}
	}
	return ""
}

type loopPlanStep struct {
	Kind           string
	Title          string
	ExecutionTitle string
	ToolID         string
	Target         string
}

type loopExecutionResult struct {
	Detail    string
	CreatedID string
	Err       error
}

type loopValidationResult struct {
	Detail string
	Err    error
}

func planMCPAction(goal string, tools []MCPTool, resources []MCPResource, prompts []MCPPrompt) (loopPlanStep, bool) {
	if selected := selectMCPResourceForGoal(goal, resources); selected != "" {
		return loopPlanStep{
			Kind:           "mcp_resource",
			Title:          "Plan MCP resource",
			ExecutionTitle: "Read MCP resource",
			ToolID:         "mcp",
			Target:         selected,
		}, true
	}
	if selected := selectMCPPromptForGoal(goal, prompts); selected != "" {
		return loopPlanStep{
			Kind:           "mcp_prompt",
			Title:          "Plan MCP prompt",
			ExecutionTitle: "Get MCP prompt",
			ToolID:         "mcp",
			Target:         selected,
		}, true
	}
	if selected := selectMCPToolForGoal(goal, tools); selected != "" {
		return loopPlanStep{
			Kind:           "mcp_call",
			Title:          "Plan MCP tool",
			ExecutionTitle: "Call MCP tool",
			ToolID:         "mcp",
			Target:         selected,
		}, true
	}
	return loopPlanStep{}, false
}

func (r *Runtime) executeMCPPlanStep(ctx context.Context, plan loopPlanStep) loopExecutionResult {
	var call MCPCall
	var err error
	switch plan.Kind {
	case "mcp_resource":
		call, err = r.ReadMCPResource(ctx, MCPResourceReadInput{ResourceID: plan.Target})
	case "mcp_prompt":
		call, err = r.GetMCPPrompt(ctx, MCPPromptGetInput{PromptID: plan.Target})
	case "mcp_call":
		call, err = r.CallMCPTool(ctx, MCPCallInput{ToolID: plan.Target})
	default:
		err = fmt.Errorf("unknown MCP plan step %q", plan.Kind)
	}
	return loopExecutionResult{
		Detail:    summarizeMCPCall(call),
		CreatedID: call.ID,
		Err:       err,
	}
}

func validateMCPExecution(plan loopPlanStep, result loopExecutionResult) loopValidationResult {
	if result.Err != nil {
		return loopValidationResult{Err: result.Err}
	}
	if strings.TrimSpace(result.CreatedID) == "" {
		return loopValidationResult{Err: errors.New("MCP result was not recorded.")}
	}
	if strings.TrimSpace(result.Detail) == "" {
		return loopValidationResult{Err: errors.New("MCP result was empty.")}
	}
	return loopValidationResult{Detail: fmt.Sprintf("%s recorded.", plan.Target)}
}

func selectMCPResourceForGoal(goal string, resources []MCPResource) string {
	if !strings.Contains(goal, "resource") && !strings.Contains(goal, "read") {
		return ""
	}
	for _, resource := range resources {
		if mcpEntryMatchesGoal(goal, resource.ID, resource.Name, resource.URI) {
			return resource.ID
		}
	}
	return ""
}

func selectMCPPromptForGoal(goal string, prompts []MCPPrompt) string {
	if !strings.Contains(goal, "prompt") {
		return ""
	}
	for _, prompt := range prompts {
		if mcpEntryMatchesGoal(goal, prompt.ID, prompt.Name) {
			return prompt.ID
		}
	}
	return ""
}

func selectMCPToolForGoal(goal string, tools []MCPTool) string {
	if !strings.Contains(goal, "tool") && !strings.Contains(goal, "call") && !strings.Contains(goal, "run") {
		return ""
	}
	for _, tool := range tools {
		if mcpEntryMatchesGoal(goal, tool.ID, tool.Name) {
			return tool.ID
		}
	}
	return ""
}

func mcpEntryMatchesGoal(goal string, values ...string) bool {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if strings.Contains(goal, value) || strings.Contains(goal, strings.ReplaceAll(value, "_", "-")) {
			return true
		}
	}
	return false
}

func summarizeMCPCall(call MCPCall) string {
	if call.State == "" {
		return ""
	}
	detail := call.ToolID
	if call.Error != "" {
		detail += " · " + call.Error
	} else if call.Output != "" {
		detail += fmt.Sprintf(" · %d bytes", len(call.Output))
	}
	return detail
}

func goalMatchesCommandTerms(goal string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(goal, term) {
			return true
		}
	}
	return false
}

func goalMatchesCommand(command string, goal string) bool {
	command = strings.ToLower(command)
	switch {
	case strings.Contains(goal, "build"):
		return strings.Contains(command, "build") || strings.Contains(command, "check") || strings.Contains(command, "test")
	case strings.Contains(goal, "test"):
		return strings.Contains(command, "test") || strings.Contains(command, "check")
	case strings.Contains(goal, "check"):
		return strings.Contains(command, "check") || strings.Contains(command, "test") || strings.Contains(command, "vet")
	default:
		return false
	}
}

func trimSymbolQueryTerm(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	for _, separator := range []string{" and ", " with ", " then ", ",", "."} {
		if index := strings.Index(lower, separator); index >= 0 {
			value = strings.TrimSpace(value[:index])
			lower = strings.ToLower(value)
		}
	}
	return value
}

func loopSummary(loop AgentLoop) string {
	counts := map[string]int{}
	for _, step := range loop.Steps {
		counts[step.State]++
	}
	switch loop.State {
	case "completed":
		if loop.Mode == "auto" {
			return fmt.Sprintf("Auto loop completed %d step(s).", counts["completed"])
		}
		return fmt.Sprintf("Completed %d step(s).", counts["completed"])
	case "waiting_approval":
		if loop.Mode == "auto" {
			return "Auto loop waiting at boundary."
		}
		return "Waiting for explicit approval."
	case "waiting_input":
		if loop.Mode == "auto" {
			return "Auto loop waiting for input."
		}
		return "Waiting for workspace or command input."
	case "canceled":
		return "Canceled."
	default:
		return "Needs attention."
	}
}

func shouldReadDiagnostics(goal string) bool {
	return strings.Contains(goal, "diagnostic") || strings.Contains(goal, "error") || strings.Contains(goal, "test") || strings.Contains(goal, "build")
}

func shouldGatherAutoEvidence(goal string, mode string) bool {
	if mode != "auto" {
		return false
	}
	return strings.Contains(goal, "fix") ||
		strings.Contains(goal, "repair") ||
		strings.Contains(goal, "refactor") ||
		strings.Contains(goal, "improve")
}

func shouldUseWorkspace(goal string) bool {
	return shouldReadDiagnostics(goal) || shouldReadSymbols(goal) || shouldReadReferences(goal) || strings.Contains(goal, "file") || strings.Contains(goal, "workspace") || strings.Contains(goal, "search") || strings.Contains(goal, "find")
}

func shouldReadSymbols(goal string) bool {
	return strings.Contains(goal, "symbol") || strings.Contains(goal, "navigate") || strings.Contains(goal, "definition")
}

func shouldReadReferences(goal string) bool {
	return strings.Contains(goal, "reference")
}

func mentionsCommand(goal string) bool {
	return strings.Contains(goal, "run ") || strings.Contains(goal, "test") || strings.Contains(goal, "build") || strings.Contains(goal, "check")
}

func mentionsEdit(goal string) bool {
	return strings.Contains(goal, "edit") || strings.Contains(goal, "change") || strings.Contains(goal, "fix") || strings.Contains(goal, "write")
}

func shouldRequestEditBoundary(goal string) bool {
	return strings.Contains(goal, "propose") ||
		strings.Contains(goal, "create") ||
		strings.Contains(goal, "edit") ||
		strings.Contains(goal, "change") ||
		strings.Contains(goal, "write")
}

func trimRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
