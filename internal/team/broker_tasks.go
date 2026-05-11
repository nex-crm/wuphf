package team

type brokerTaskMutationSnapshot struct {
	tasks       []teamTask
	channels    []teamChannel
	messages    []channelMessage
	agentIssues []agentIssueRecord
	actions     []officeActionLog
	watchdogs   []watchdogAlert
	scheduler   []schedulerJob
	lifecycle   map[LifecycleState][]string
	counter     int
}

func snapshotBrokerTaskMutationLocked(b *Broker) brokerTaskMutationSnapshot {
	return brokerTaskMutationSnapshot{
		tasks:       cloneTeamTasksForRollback(b.tasks),
		channels:    cloneTeamChannelsForRollback(b.channels),
		messages:    cloneChannelMessagesForRollback(b.messages),
		agentIssues: append([]agentIssueRecord(nil), b.agentIssues...),
		actions:     cloneOfficeActionsForRollback(b.actions),
		watchdogs:   append([]watchdogAlert(nil), b.watchdogs...),
		scheduler:   append([]schedulerJob(nil), b.scheduler...),
		lifecycle:   cloneLifecycleIndexForRollback(b.lifecycleIndex),
		counter:     b.counter,
	}
}

func (snapshot brokerTaskMutationSnapshot) restore(b *Broker) {
	b.tasks = cloneTeamTasksForRollback(snapshot.tasks)
	b.channels = cloneTeamChannelsForRollback(snapshot.channels)
	b.messages = cloneChannelMessagesForRollback(snapshot.messages)
	b.agentIssues = append([]agentIssueRecord(nil), snapshot.agentIssues...)
	b.actions = cloneOfficeActionsForRollback(snapshot.actions)
	b.watchdogs = append([]watchdogAlert(nil), snapshot.watchdogs...)
	b.scheduler = append([]schedulerJob(nil), snapshot.scheduler...)
	b.lifecycleIndex = cloneLifecycleIndexForRollback(snapshot.lifecycle)
	b.counter = snapshot.counter
}

func cloneLifecycleIndexForRollback(index map[LifecycleState][]string) map[LifecycleState][]string {
	if len(index) == 0 {
		return nil
	}
	out := make(map[LifecycleState][]string, len(index))
	for state, ids := range index {
		out[state] = append([]string(nil), ids...)
	}
	return out
}

func cloneTeamTasksForRollback(tasks []teamTask) []teamTask {
	if len(tasks) == 0 {
		return nil
	}
	out := make([]teamTask, len(tasks))
	for i := range tasks {
		out[i] = cloneTeamTaskForRollback(tasks[i])
	}
	return out
}

func cloneTeamChannelsForRollback(channels []teamChannel) []teamChannel {
	if len(channels) == 0 {
		return nil
	}
	out := append([]teamChannel(nil), channels...)
	for i := range out {
		out[i].Members = append([]string(nil), channels[i].Members...)
		out[i].Disabled = append([]string(nil), channels[i].Disabled...)
		if channels[i].Surface != nil {
			surface := *channels[i].Surface
			out[i].Surface = &surface
		}
	}
	return out
}

func cloneChannelMessagesForRollback(messages []channelMessage) []channelMessage {
	if len(messages) == 0 {
		return nil
	}
	out := append([]channelMessage(nil), messages...)
	for i := range out {
		out[i].Tagged = append([]string(nil), messages[i].Tagged...)
		out[i].Reactions = append([]messageReaction(nil), messages[i].Reactions...)
		if messages[i].Usage != nil {
			usage := *messages[i].Usage
			out[i].Usage = &usage
		}
	}
	return out
}

func cloneOfficeActionsForRollback(actions []officeActionLog) []officeActionLog {
	if len(actions) == 0 {
		return nil
	}
	out := append([]officeActionLog(nil), actions...)
	for i := range out {
		out[i].SignalIDs = append([]string(nil), actions[i].SignalIDs...)
	}
	return out
}
