package tui

type ControlAction string

const (
	CmdStartCoordinator     ControlAction = "start_coordinator"
	CmdStopCoordinator      ControlAction = "stop_coordinator"
	CmdAddResolver          ControlAction = "add_resolver"
	CmdRemoveResolver       ControlAction = "remove_resolver"
	CmdAddWorker            ControlAction = "add_worker"
	CmdRemoveWorker         ControlAction = "remove_worker"
	CmdAddEnhancer          ControlAction = "add_enhancer"
	CmdRemoveEnhancer       ControlAction = "remove_enhancer"
	CmdAddCleaner           ControlAction = "add_cleaner"
	CmdRemoveCleaner        ControlAction = "remove_cleaner"
	CmdResetFailed          ControlAction = "reset_failed"
	CmdRestartWorker        ControlAction = "restart_worker"
	CmdShutdown             ControlAction = "shutdown"
)

type ControlCmd struct {
	Action   ControlAction
	WorkerID string
}

func NewControlChannel() chan ControlCmd {
	return make(chan ControlCmd, 10)
}
