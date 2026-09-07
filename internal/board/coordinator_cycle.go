package board

// coordinatorMemberCycle binds a waiting member only from committed producer
// metadata under the caller's task revision and checkpoint CAS. It does not
// create a task execution or reset a cycle that was already observed.
func coordinatorMemberCycle(id string, old CoordinatorMember, s Snapshot) (string, bool, error) {
	emptyCycle := ReviewDigest([]byte(id + "\n"))
	awaiting := old.AwaitingStart
	// Schema 1 originally stored an empty-start digest without a flag. Only
	// an unstarted, inactive cursor can be upgraded; retain its immutable history.
	if old.Cycle == emptyCycle && (old.State == "todo" || old.State == "backlog") && old.Dispatch == nil && old.Review == nil && old.DeliveryCommit == "" {
		awaiting = true
	}
	cycle := planCycle(s)
	if cycle == old.Cycle {
		return cycle, awaiting, nil
	}
	startedState := s.Entry.State == "working" || s.Entry.State == "review" || s.Entry.State == "done" || s.Entry.State == "archived"
	if !awaiting || old.Cycle != emptyCycle || s.Revision <= old.Revision || !startedState || MetadataFrom(s.Text, FieldStartedAt) == "" || MetadataFrom(s.Text, FieldOwner) == "" {
		return "", false, coordinatorError("member execution cycle changed without first-start facts: " + id)
	}
	return cycle, false, nil
}
