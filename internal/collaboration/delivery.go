package collaboration

import "errors"

type DeliveryResult struct {
	Status string
	Cause  string
}

// CompleteDelivery never sends a message. A retry of the same event reads
// the existing record; a pending/uncertain result needs operator inspection.
func CompleteDelivery(store Store, prepared PreparedEvent, result DeliveryResult) (Assignment, error) {
	if !prepared.Fresh || prepared.Event.Delivery == "not-required" {
		return prepared.Assignment, nil
	}
	switch result.Status {
	case "submitted", "queued", "received", "uncertain", "refused":
	default:
		return prepared.Assignment, errors.New("invalid collaboration delivery result")
	}
	current, err := store.Read(prepared.Assignment.ID)
	if err != nil {
		return current, err
	}
	return store.Update(current.ID, current.Generation, func(a *Assignment) error {
		for i := range a.Events {
			if a.Events[i].ID == prepared.Event.ID {
				if a.Events[i].Delivery != "pending" {
					return errors.New("delivery already recorded")
				}
				a.Events[i].Delivery, a.Events[i].Cause = result.Status, result.Cause
				if a.Phase != Finished && (result.Status == "refused" || result.Status == "uncertain") {
					a.Phase = Escalated
				}
				return nil
			}
		}
		return errors.New("pending event not found")
	})
}
