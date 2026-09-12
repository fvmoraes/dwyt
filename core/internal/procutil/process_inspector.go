package procutil

import "fmt"

const maxPIDRecordExecInspectionAttempts = 3

// errProcessExecutedDuringInspect identifies the safe subset of process
// identity changes: exec replaces an executable without changing the process
// start token. It wraps the generic change error so existing fail-closed
// classification remains intact.
var errProcessExecutedDuringInspect = fmt.Errorf("%w: executable changed during inspection", errProcessChangedDuringInspect)
