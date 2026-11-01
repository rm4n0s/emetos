package java

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// What jdtls tells the client besides the protocol (SPEC §10.34 "Java"): its status and progress, which the status item shows, the
// questions it asks (language/actionableNotification), and the events of its projects.

// The notifications of the server.
const (
	statusMethod            = "language/status"
	progressMethod          = "language/progressReport"
	actionableMethod        = "language/actionableNotification"
	eventNotificationMethod = "language/eventNotification"
)

// serverState is what the server told about itself since its last start. It is changed on the UI thread only.
type serverState struct {
	// kind and message are the last language/status: Starting, Started, ServiceReady, Message, Error or ProjectStatus.
	kind, message string
	// tasks are the tasks of language/progressReport that are not complete, by id, and order the ids in the order they came.
	tasks map[string]string
	order []string
	// ready says that the server told ServiceReady, and projectWarning that a ProjectStatus said WARNING.
	ready, projectWarning bool
}

// health is how the item shows the state: busy while the server starts or has a task, an error or warning with its message, else ok.
func (s serverState) health() langserver.Health {
	h := langserver.Health{Level: langserver.HealthOK}
	switch {
	case s.kind == "Error":
		h.Level, h.Message = langserver.HealthError, s.message
	case s.projectWarning:
		h.Level, h.Message = langserver.HealthWarning, "A project has problems with its configuration."
	}
	switch {
	case len(s.order) > 0:
		h.Busy, h.Message = true, s.tasks[s.order[len(s.order)-1]]
	case s.kind != "" && !s.ready && s.kind != "Error":
		h.Busy = true
		if s.message != "" {
			h.Message = s.message
		}
	}
	return h
}

// show gives the state to the status item.
func (j *java) show() {
	if j.ctl != nil {
		j.ctl.SetHealth(j.status.health())
	}
}

// serverChanged forgets what the last start told when the server starts or stops (the item forgets its health by itself).
func (j *java) serverChanged(st extension.ServerStatus) {
	if st.State != extension.ServerRunning {
		j.status = serverState{}
	}
}

// onStatus follows language/status.
func (j *java) onStatus(params json.RawMessage) {
	var p struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		j.api.Log().Warn("the status of jdtls cannot be read", "err", err)
		return
	}
	switch p.Type {
	case "ProjectStatus":
		j.status.projectWarning = strings.EqualFold(p.Message, "WARNING")
	case "ServiceReady":
		j.status.ready = true
		j.status.kind, j.status.message = p.Type, p.Message
	case "Starting":
		j.status.ready = false
		j.status.kind, j.status.message = p.Type, p.Message
	default:
		j.status.kind, j.status.message = p.Type, p.Message
	}
	j.show()
}

// onProgress follows language/progressReport: a task is shown until it is complete.
func (j *java) onProgress(params json.RawMessage) {
	var p struct {
		ID       string `json:"id"`
		Task     string `json:"task"`
		SubTask  string `json:"subTask"`
		Status   string `json:"status"`
		Complete bool   `json:"complete"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		j.api.Log().Warn("the progress of jdtls cannot be read", "err", err)
		return
	}
	if j.status.tasks == nil {
		j.status.tasks = map[string]string{}
	}
	if p.Complete {
		delete(j.status.tasks, p.ID)
		for i, id := range j.status.order {
			if id == p.ID {
				j.status.order = append(j.status.order[:i], j.status.order[i+1:]...)
				break
			}
		}
		j.show()
		return
	}
	text := strings.TrimSpace(p.Task)
	if sub := strings.TrimSpace(p.SubTask); sub != "" && sub != text {
		text += ": " + sub
	}
	if text == "" {
		text = strings.TrimSpace(p.Status)
	}
	if _, known := j.status.tasks[p.ID]; !known {
		j.status.order = append(j.status.order, p.ID)
	}
	j.status.tasks[p.ID] = text
	j.show()
}

// The severities of language/actionableNotification are the MessageType of the protocol.
const (
	severityError   = 1
	severityWarning = 2
	severityInfo    = 3
)

// onActionable follows language/actionableNotification: the question or message of the server with a button for each command it offers.
func (j *java) onActionable(params json.RawMessage) {
	var p struct {
		Severity int    `json:"severity"`
		Message  string `json:"message"`
		Commands []struct {
			Title     string            `json:"title"`
			Command   string            `json:"command"`
			Arguments []json.RawMessage `json:"arguments"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		j.api.Log().Warn("a question of jdtls cannot be read", "err", err)
		return
	}
	if p.Message == "" {
		return
	}
	var actions []extension.NotificationAction
	for _, c := range p.Commands {
		if c.Title == "" || c.Command == "" {
			continue
		}
		command, args := c.Command, c.Arguments
		actions = append(actions, extension.NotificationAction{Label: c.Title, Run: func() { j.runServerOffer(command, args) }})
	}
	switch p.Severity {
	case severityError:
		j.api.Notifications().Error(p.Message, actions...)
	case severityWarning:
		j.api.Notifications().Warn(p.Message, actions...)
	case severityInfo:
		j.api.Notifications().Info(p.Message, actions...)
	default:
		j.api.Log().Debug("a message of jdtls", "message", p.Message)
	}
}

// Fixed values of the questions the server asks.
const (
	projectConfigurationStatusCommand = "java.projectConfiguration.status"
	featureAutomatic                  = "automatic"
	featureDisabled                   = "disabled"
	featureInteractive                = "interactive"
)

// runServerOffer runs the command of a button of the server. The commands of the client the server knows are the pack's own
// (java.projectConfiguration.update, java.projectConfiguration.status); one that the server itself executes goes back to it.
func (j *java) runServerOffer(command string, args []json.RawMessage) {
	switch command {
	case UpdateProjectConfigurationCommand:
		j.updateProjectConfiguration(documentURI(args))
		return
	case projectConfigurationStatusCommand:
		j.setUpdateBuildConfiguration(documentURI(args), featureStatus(args))
		return
	}
	known, err := j.api.LanguageServers().Commands(ServerID)
	if err != nil {
		j.api.Notifications().Error(describe(err))
		return
	}
	for _, name := range known {
		if name != command {
			continue
		}
		list := make([]any, len(args))
		for i, a := range args {
			list[i] = a
		}
		if err := j.api.LanguageServers().ExecuteCommand(j.ctx, ServerID, command, list, func(err error) {
			if err != nil {
				j.api.Log().Warn("a command of jdtls failed", "command", command, "err", err)
				j.api.Notifications().Error("The language server failed: " + describe(err))
			}
		}); err != nil && !benign(err) {
			j.api.Notifications().Error(describe(err))
		}
		return
	}
	j.api.Log().Debug("jdtls offered a command that Emetos does not run", "command", command)
	j.api.Notifications().Info("Emetos cannot run " + command + ", which the language server offered.")
}

// documentURI is the URI of the first argument of a command of the server: a TextDocumentIdentifier or a URI as a string; "" for neither.
func documentURI(args []json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var id struct {
		URI string `json:"uri"`
	}
	if json.Unmarshal(args[0], &id) == nil && id.URI != "" {
		return id.URI
	}
	var s string
	if json.Unmarshal(args[0], &s) == nil {
		return s
	}
	return ""
}

// featureStatus is the second argument of java.projectConfiguration.status: the status as its name, or as the number of the enum of the
// server (0 disabled, 1 interactive, 2 automatic).
func featureStatus(args []json.RawMessage) string {
	if len(args) < 2 {
		return ""
	}
	var s string
	if json.Unmarshal(args[1], &s) == nil {
		return strings.ToLower(s)
	}
	var n int
	if json.Unmarshal(args[1], &n) == nil {
		switch n {
		case 0:
			return featureDisabled
		case 1:
			return featureInteractive
		case 2:
			return featureAutomatic
		}
	}
	return strconv.Itoa(n)
}

// setUpdateBuildConfiguration is the answer Always or Never to the question about a changed build file: it keeps the choice in the
// user's settings (java.configuration.updateBuildConfiguration) and, for Always, updates the project now. The setting is the host's: a
// client of a hosted workspace is told so.
func (j *java) setUpdateBuildConfiguration(uri, status string) {
	if status != featureAutomatic && status != featureDisabled && status != featureInteractive {
		j.api.Log().Debug("jdtls asked for a status of the build configuration that is not known", "status", status)
		return
	}
	if j.api.Origins().Current() != extension.HostOrigin {
		j.api.Notifications().Info("The choice about the build files is a setting of the host and cannot be changed from here.")
		return
	}
	if err := j.api.Settings().Update(UpdateBuildConfigurationSetting, status); err != nil && !benign(err) {
		j.api.Log().Warn("the choice about the build files cannot be saved", "err", err)
		j.api.Notifications().Error("The choice cannot be saved: " + describe(err))
		return
	}
	if status == featureAutomatic && uri != "" {
		j.updateProjectConfiguration(uri)
	}
}

// onEvent follows language/eventNotification, the events of the projects of the server. Emetos has no view of the projects, so the
// events are only logged.
func (j *java) onEvent(params json.RawMessage) {
	var p struct {
		EventType int `json:"eventType"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	j.api.Log().Debug("jdtls told an event of its projects", "eventType", p.EventType)
}
