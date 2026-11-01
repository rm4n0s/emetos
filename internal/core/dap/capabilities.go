package dap

// Capabilities are the capabilities of a debug adapter, the body of the initialize response and of the capabilities
// event. Every member is false or empty until the adapter says otherwise; the client keeps the merged value (Client.Capabilities).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugProtocol.d.ts (Capabilities)
type Capabilities struct {
	SupportsConfigurationDoneRequest      bool                         `json:"supportsConfigurationDoneRequest,omitempty"`
	SupportsFunctionBreakpoints           bool                         `json:"supportsFunctionBreakpoints,omitempty"`
	SupportsConditionalBreakpoints        bool                         `json:"supportsConditionalBreakpoints,omitempty"`
	SupportsHitConditionalBreakpoints     bool                         `json:"supportsHitConditionalBreakpoints,omitempty"`
	SupportsEvaluateForHovers             bool                         `json:"supportsEvaluateForHovers,omitempty"`
	ExceptionBreakpointFilters            []ExceptionBreakpointsFilter `json:"exceptionBreakpointFilters,omitempty"`
	SupportsStepBack                      bool                         `json:"supportsStepBack,omitempty"`
	SupportsSetVariable                   bool                         `json:"supportsSetVariable,omitempty"`
	SupportsRestartFrame                  bool                         `json:"supportsRestartFrame,omitempty"`
	SupportsGotoTargetsRequest            bool                         `json:"supportsGotoTargetsRequest,omitempty"`
	SupportsStepInTargetsRequest          bool                         `json:"supportsStepInTargetsRequest,omitempty"`
	SupportsCompletionsRequest            bool                         `json:"supportsCompletionsRequest,omitempty"`
	CompletionTriggerCharacters           []string                     `json:"completionTriggerCharacters,omitempty"`
	SupportsModulesRequest                bool                         `json:"supportsModulesRequest,omitempty"`
	AdditionalModuleColumns               []ColumnDescriptor           `json:"additionalModuleColumns,omitempty"`
	SupportedChecksumAlgorithms           []string                     `json:"supportedChecksumAlgorithms,omitempty"`
	SupportsRestartRequest                bool                         `json:"supportsRestartRequest,omitempty"`
	SupportsExceptionOptions              bool                         `json:"supportsExceptionOptions,omitempty"`
	SupportsValueFormattingOptions        bool                         `json:"supportsValueFormattingOptions,omitempty"`
	SupportsExceptionInfoRequest          bool                         `json:"supportsExceptionInfoRequest,omitempty"`
	SupportTerminateDebuggee              bool                         `json:"supportTerminateDebuggee,omitempty"`
	SupportSuspendDebuggee                bool                         `json:"supportSuspendDebuggee,omitempty"`
	SupportsDelayedStackTraceLoading      bool                         `json:"supportsDelayedStackTraceLoading,omitempty"`
	SupportsLoadedSourcesRequest          bool                         `json:"supportsLoadedSourcesRequest,omitempty"`
	SupportsLogPoints                     bool                         `json:"supportsLogPoints,omitempty"`
	SupportsTerminateThreadsRequest       bool                         `json:"supportsTerminateThreadsRequest,omitempty"`
	SupportsSetExpression                 bool                         `json:"supportsSetExpression,omitempty"`
	SupportsTerminateRequest              bool                         `json:"supportsTerminateRequest,omitempty"`
	SupportsDataBreakpoints               bool                         `json:"supportsDataBreakpoints,omitempty"`
	SupportsReadMemoryRequest             bool                         `json:"supportsReadMemoryRequest,omitempty"`
	SupportsWriteMemoryRequest            bool                         `json:"supportsWriteMemoryRequest,omitempty"`
	SupportsDisassembleRequest            bool                         `json:"supportsDisassembleRequest,omitempty"`
	SupportsCancelRequest                 bool                         `json:"supportsCancelRequest,omitempty"`
	SupportsBreakpointLocationsRequest    bool                         `json:"supportsBreakpointLocationsRequest,omitempty"`
	SupportsClipboardContext              bool                         `json:"supportsClipboardContext,omitempty"`
	SupportsSteppingGranularity           bool                         `json:"supportsSteppingGranularity,omitempty"`
	SupportsInstructionBreakpoints        bool                         `json:"supportsInstructionBreakpoints,omitempty"`
	SupportsExceptionFilterOptions        bool                         `json:"supportsExceptionFilterOptions,omitempty"`
	SupportsSingleThreadExecutionRequests bool                         `json:"supportsSingleThreadExecutionRequests,omitempty"`
	SupportsDataBreakpointBytes           bool                         `json:"supportsDataBreakpointBytes,omitempty"`
	BreakpointModes                       []BreakpointMode             `json:"breakpointModes,omitempty"`
	SupportsANSIStyling                   bool                         `json:"supportsANSIStyling,omitempty"`
}
