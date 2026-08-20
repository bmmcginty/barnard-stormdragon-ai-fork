package gumble

// ContextActions is a map of ContextActions.
type ContextActions map[string]*ContextAction

func (c ContextActions) create(client *Client, action string) *ContextAction {
	contextAction := &ContextAction{
		Name:   action,
		client: client,
	}
	c[action] = contextAction
	return contextAction
}
