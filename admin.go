package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/uiterm"
	"github.com/nsf/termbox-go"
)

type adminPrompt struct {
	label  string
	action func(string)
}

type adminItem struct {
	label    string
	children []uiterm.TreeItem
	action   func()
}

func (i adminItem) String() string {
	return i.label
}

func (i adminItem) TreeItemStyle(fg, bg uiterm.Attribute, active bool) (uiterm.Attribute, uiterm.Attribute) {
	if len(i.children) > 0 {
		fg |= uiterm.AttrBold
	}
	if active {
		fg, bg = bg, fg
	}
	return fg, bg
}

func (b *Barnard) OnAdminMenuPress(ui *uiterm.Ui, key uiterm.Key) {
	b.OpenAdminMenu()
}

func (b *Barnard) OnAdminEscape(ui *uiterm.Ui, key uiterm.Key) {
	if b.pendingAdminPrompt != nil {
		b.pendingAdminPrompt = nil
		b.UiInput.Text = ""
		b.AddOutputLine("Admin: prompt canceled")
		b.UpdateInputStatus(fmt.Sprintf("[%s]", b.Client.Self.Channel.Name))
		return
	}
	if b.Ui.Active() == uiViewAdmin {
		b.CloseAdminMenu(ui)
		b.AddOutputLine("Admin: closed")
	}
}

func (b *Barnard) closeAdminAction() {
	b.OnAdminEscape(b.Ui, uiterm.KeyEsc)
}

func (b *Barnard) OpenAdminMenu() {
	if b.Client == nil || b.Client.Self == nil {
		b.AddOutputLine("Admin: not connected")
		return
	}
	b.adminReturnItem = b.UiTree.ActiveItem()
	b.adminTargetUser = b.selectedUser
	b.adminTargetChan = b.Client.Self.Channel
	if b.Ui.Active() == uiViewTree {
		switch item := b.UiTree.ActiveItem().(type) {
		case TreeItem:
			if item.User != nil {
				b.adminTargetUser = item.User
				b.adminTargetChan = item.User.Channel
			}
			if item.Channel != nil {
				b.adminTargetChan = item.Channel
				b.adminTargetUser = nil
			}
		}
	}
	if b.adminTargetChan != nil {
		b.adminTargetChan.RequestPermission()
	}
	if root := b.Client.Channels[0]; root != nil && root != b.adminTargetChan {
		root.RequestPermission()
	}
	b.UiAdmin.Rebuild()
	b.Ui.SetActive(uiViewAdmin)
	width, height := termboxSize()
	b.OnUiResize(b.Ui, width, height)
	if b.adminTargetUser != nil {
		b.AddOutputLine(fmt.Sprintf("Admin: opened for user %s", b.adminTargetUser.Name))
	} else if b.adminTargetChan != nil {
		b.AddOutputLine(fmt.Sprintf("Admin: opened for channel %s", b.adminTargetChan.Name))
	} else {
		b.AddOutputLine("Admin: opened")
	}
	b.Ui.Refresh()
}

func (b *Barnard) CloseAdminMenu(ui *uiterm.Ui) {
	b.Ui.SetActive(uiViewTree)
	b.UiTree.SetActiveItem(b.adminReturnItem, sameUserChannelTreeItem)
	b.adminReturnItem = nil
	width, height := termboxSize()
	b.OnUiResize(ui, width, height)
	ui.Refresh()
}

func termboxSize() (int, int) {
	width, height := termbox.Size()
	return width, height
}

func (b *Barnard) AdminItemBuild(item uiterm.TreeItem) []uiterm.TreeItem {
	if item != nil {
		if admin, ok := item.(adminItem); ok {
			return admin.children
		}
		return nil
	}

	target := "server"
	if b.adminTargetUser != nil {
		target = "user " + b.adminTargetUser.Name
	} else if b.adminTargetChan != nil {
		target = "channel " + b.adminTargetChan.Name
	}

	items := []uiterm.TreeItem{
		adminItem{label: "Action target: " + target},
		adminItem{label: "Information actions", children: b.informationItems()},
	}
	if children := b.adminUserItems(); len(children) > 0 {
		items = append(items, adminItem{label: "Selected user admin actions", children: children})
	}
	if children := b.adminChannelItems(); len(children) > 0 {
		items = append(items, adminItem{label: "Channel admin actions", children: children})
	}
	if children := b.adminBanItems(); len(children) > 0 {
		items = append(items, adminItem{label: "Ban list admin actions", children: children})
	}
	if children := b.adminRegisteredUserItems(); len(children) > 0 {
		items = append(items, adminItem{label: "Registered user admin actions", children: children})
	}
	if children := b.adminACLItems(); len(children) > 0 {
		items = append(items, adminItem{label: "ACL admin actions", children: children})
	}
	if children := b.adminContextActionItems(); len(children) > 0 {
		items = append(items, adminItem{label: "Context actions", children: children})
	}
	items = append(items, adminItem{label: "Close actions menu", action: b.closeAdminAction})
	return items
}

func (b *Barnard) AdminItemKeyPress(ui *uiterm.Ui, tree *uiterm.Tree, item uiterm.TreeItem, key uiterm.Key) {
	if isAdminEscapeKey(key) {
		b.OnAdminEscape(ui, key)
		return
	}
	if key != uiterm.KeyEnter {
		return
	}
	admin, ok := item.(adminItem)
	if !ok || admin.action == nil {
		return
	}
	if !b.withValidAdminTargets(admin.action) {
		b.AddOutputLine("Admin: action target is no longer available")
	}
	b.UiAdmin.Rebuild()
	b.Ui.Refresh()
}

func (b *Barnard) AdminItemCharacter(ui *uiterm.Ui, tree *uiterm.Tree, item uiterm.TreeItem, ch rune) {
	if isAdminEscapeCharacter(ch) {
		b.OnAdminEscape(ui, uiterm.KeyEsc)
	}
}

func isAdminEscapeKey(key uiterm.Key) bool {
	switch key {
	case uiterm.KeyEsc, uiterm.KeyAltEsc, uiterm.KeyAltArrowUp, uiterm.KeyAltArrowDown:
		return true
	default:
		return false
	}
}

func isAdminEscapeCharacter(ch rune) bool {
	return ch == rune(uiterm.KeyEsc)
}

func (b *Barnard) informationItems() []uiterm.TreeItem {
	items := []uiterm.TreeItem{}
	if u := b.adminTargetUser; u != nil {
		items = append(items,
			adminItem{label: "Request user stats for " + u.Name, action: func() {
				u.RequestStats()
				b.AddOutputLine(fmt.Sprintf("Actions: requested stats for %s", u.Name))
				b.showOutputAfterInfoRequest()
			}},
			adminItem{label: "Request user comment for " + u.Name, action: func() {
				u.RequestComment()
				b.AddOutputLine(fmt.Sprintf("Actions: requested comment for %s", u.Name))
				b.showOutputAfterInfoRequest()
			}},
		)
	}
	if ch := b.adminTargetChan; ch != nil {
		items = append(items,
			adminItem{label: "Move self to " + ch.Name, action: func() {
				b.Client.Self.Move(ch)
				b.AddOutputLine(fmt.Sprintf("Actions: moving to %s", ch.Name))
			}},
			adminItem{label: "Request channel description for " + ch.Name, action: func() {
				ch.RequestDescription()
				b.AddOutputLine(fmt.Sprintf("Actions: requested description for %s", ch.Name))
				b.showOutputAfterInfoRequest()
			}},
			adminItem{label: "Request channel permissions for " + ch.Name, action: func() {
				ch.RequestPermission()
				b.AddOutputLine(fmt.Sprintf("Actions: requested permissions for %s", ch.Name))
				b.showOutputAfterInfoRequest()
			}},
		)
	}
	if len(items) == 0 {
		return []uiterm.TreeItem{adminItem{label: "No information actions available"}}
	}
	return items
}

func (b *Barnard) showOutputAfterInfoRequest() {
	b.Ui.SetActive(uiViewInput)
	width, height := termboxSize()
	b.OnUiResize(b.Ui, width, height)
}

func (b *Barnard) adminUserItems() []uiterm.TreeItem {
	u := b.adminTargetUser
	if u == nil {
		return nil
	}
	items := []uiterm.TreeItem{}
	if b.adminCanRoot(gumble.PermissionKick) {
		items = append(items, adminItem{label: "Kick " + u.Name, action: func() {
			b.promptAdmin("Kick reason for "+u.Name, func(reason string) {
				u.Kick(reason)
				b.AddOutputLine(fmt.Sprintf("Admin: kick sent for %s", u.Name))
			})
		}})
	}
	if b.adminCanRoot(gumble.PermissionBan) {
		items = append(items, adminItem{label: "Ban " + u.Name, action: func() {
			b.promptAdmin("Ban reason for "+u.Name, func(reason string) {
				u.Ban(reason)
				b.AddOutputLine(fmt.Sprintf("Admin: ban sent for %s", u.Name))
			})
		}})
	}
	if b.adminCanChannel(u.Channel, gumble.PermissionMuteDeafen) {
		items = append(items, adminItem{label: fmt.Sprintf("Server mute: %s", onOff(u.Muted)), action: func() {
			u.SetMuted(!u.Muted)
			b.AddOutputLine(fmt.Sprintf("Admin: server mute toggled for %s", u.Name))
		}})
		items = append(items, adminItem{label: fmt.Sprintf("Server deafen: %s", onOff(u.Deafened)), action: func() {
			u.SetDeafened(!u.Deafened)
			b.AddOutputLine(fmt.Sprintf("Admin: server deafen toggled for %s", u.Name))
		}})
		items = append(items, adminItem{label: fmt.Sprintf("Suppress in channel: %s", onOff(u.Suppressed)), action: func() {
			u.SetSuppressed(!u.Suppressed)
			b.AddOutputLine(fmt.Sprintf("Admin: suppress toggled for %s", u.Name))
		}})
		items = append(items, adminItem{label: fmt.Sprintf("Priority speaker: %s", onOff(u.PrioritySpeaker)), action: func() {
			u.SetPrioritySpeaker(!u.PrioritySpeaker)
			b.AddOutputLine(fmt.Sprintf("Admin: priority speaker toggled for %s", u.Name))
		}})
	}
	if b.adminCanChannel(b.Client.Self.Channel, gumble.PermissionMove) {
		items = append(items, adminItem{label: "Move user to current channel", action: func() {
			u.Move(b.Client.Self.Channel)
			b.AddOutputLine(fmt.Sprintf("Admin: move sent for %s to %s", u.Name, b.Client.Self.Channel.Name))
		}})
	}
	if b.adminCanRoot(gumble.PermissionRegister) {
		items = append(items, adminItem{label: "Register user", action: func() {
			u.Register()
			b.AddOutputLine(fmt.Sprintf("Admin: register sent for %s", u.Name))
		}})
	}
	return items
}

func (b *Barnard) adminChannelItems() []uiterm.TreeItem {
	ch := b.adminTargetChan
	if ch == nil {
		return nil
	}
	items := []uiterm.TreeItem{}
	if b.adminCanChannel(ch, gumble.PermissionMakeChannel) {
		items = append(items, adminItem{label: "Create child channel", action: func() {
			b.promptAdmin("New channel name under "+ch.Name, func(name string) {
				name = strings.TrimSpace(name)
				if name == "" {
					b.AddOutputLine("Admin: channel name required")
					return
				}
				ch.Add(name, false)
				b.AddOutputLine(fmt.Sprintf("Admin: create channel sent for %s", name))
			})
		}})
	}
	if b.adminCanChannel(ch, gumble.PermissionMakeTemporaryChannel) {
		items = append(items, adminItem{label: "Create temporary child channel", action: func() {
			b.promptAdmin("Temporary channel name under "+ch.Name, func(name string) {
				name = strings.TrimSpace(name)
				if name == "" {
					b.AddOutputLine("Admin: channel name required")
					return
				}
				ch.Add(name, true)
				b.AddOutputLine(fmt.Sprintf("Admin: create temporary channel sent for %s", name))
			})
		}})
	}
	if b.adminCanChannel(ch, gumble.PermissionWrite) {
		items = append(items, adminItem{label: "Rename channel", action: func() {
			b.promptAdmin("New name for "+ch.Name, func(name string) {
				name = strings.TrimSpace(name)
				if name == "" {
					b.AddOutputLine("Admin: channel name required")
					return
				}
				ch.SetName(name)
				b.AddOutputLine(fmt.Sprintf("Admin: rename channel sent for %s", ch.Name))
			})
		}})
		items = append(items, adminItem{label: "Set channel description", action: func() {
			b.promptAdmin("Description for "+ch.Name, func(description string) {
				ch.SetDescription(description)
				b.AddOutputLine(fmt.Sprintf("Admin: channel description sent for %s", ch.Name))
			})
		}})
		items = append(items, adminItem{label: "Set max users", action: func() {
			b.promptAdmin("Max users for "+ch.Name+" (0 for server default)", func(text string) {
				maxUsers, err := strconv.ParseUint(strings.TrimSpace(text), 10, 32)
				if err != nil {
					b.AddOutputLine("Admin: max users must be a number")
					return
				}
				ch.SetMaxUsers(uint32(maxUsers))
				b.AddOutputLine(fmt.Sprintf("Admin: max users sent for %s", ch.Name))
			})
		}})
		items = append(items, adminItem{label: "Delete channel", action: func() {
			if ch.IsRoot() {
				b.AddOutputLine("Admin: cannot delete the root channel")
				return
			}
			b.promptAdmin("Type delete to remove channel "+ch.Name, func(text string) {
				if strings.TrimSpace(strings.ToLower(text)) != "delete" {
					b.AddOutputLine("Admin: delete channel canceled")
					return
				}
				ch.Remove()
				b.AddOutputLine(fmt.Sprintf("Admin: delete channel sent for %s", ch.Name))
			})
		}})
	}
	if len(ch.Links) > 0 && b.adminCanChannel(ch, gumble.PermissionLinkChannel) {
		items = append(items, adminItem{label: "Unlink all channels", action: func() {
			ch.Unlink()
			b.AddOutputLine(fmt.Sprintf("Admin: unlink all sent for %s", ch.Name))
		}})
	}
	return items
}

func (b *Barnard) adminBanItems() []uiterm.TreeItem {
	if !b.adminCanRoot(gumble.PermissionBan) {
		return nil
	}
	items := []uiterm.TreeItem{
		adminItem{label: "Request ban list", action: func() {
			b.Client.RequestBanList()
			b.AddOutputLine("Admin: requested ban list")
		}},
		adminItem{label: "Add manual ban", action: func() {
			b.promptAdmin("Ban as ip/mask minutes reason", b.addManualBan)
		}},
	}
	for index, ban := range b.adminBanList {
		label := fmt.Sprintf("Ban %d: %s %s", index+1, ban.Address.String(), ban.Reason)
		i := index
		items = append(items, adminItem{label: label, children: []uiterm.TreeItem{
			adminItem{label: "Unban this entry", action: func() {
				b.unbanIndex(i)
			}},
		}})
	}
	return items
}

func (b *Barnard) adminRegisteredUserItems() []uiterm.TreeItem {
	if !b.adminCanRoot(gumble.PermissionRegister) {
		return nil
	}
	items := []uiterm.TreeItem{
		adminItem{label: "Request registered user list", action: func() {
			b.Client.RequestUserList()
			b.AddOutputLine("Admin: requested registered user list")
		}},
	}
	for _, user := range b.adminUserList {
		u := user
		label := fmt.Sprintf("Registered user %d: %s", u.UserID, u.Name)
		items = append(items, adminItem{label: label, children: []uiterm.TreeItem{
			adminItem{label: "Rename " + u.Name, action: func() {
				b.promptAdmin("New name for "+u.Name, func(name string) {
					name = strings.TrimSpace(name)
					if name == "" {
						b.AddOutputLine("Admin: registered user name required")
						return
					}
					u.SetName(name)
					b.Client.Send(b.adminUserList)
					b.AddOutputLine(fmt.Sprintf("Admin: rename registered user sent for %d", u.UserID))
				})
			}},
			adminItem{label: "Deregister " + u.Name, action: func() {
				b.promptAdmin("Type deregister to remove "+u.Name, func(text string) {
					if strings.TrimSpace(strings.ToLower(text)) != "deregister" {
						b.AddOutputLine("Admin: deregister canceled")
						return
					}
					u.Deregister()
					b.Client.Send(b.adminUserList)
					b.AddOutputLine(fmt.Sprintf("Admin: deregister sent for %s", u.Name))
				})
			}},
		}})
	}
	return items
}

func (b *Barnard) adminACLItems() []uiterm.TreeItem {
	ch := b.adminTargetChan
	if ch == nil {
		return nil
	}
	if !b.adminCanChannel(ch, gumble.PermissionWrite) {
		return nil
	}
	items := []uiterm.TreeItem{
		adminItem{label: "Request ACLs for " + ch.Name, action: func() {
			ch.RequestACL()
			b.AddOutputLine(fmt.Sprintf("Admin: requested ACLs for %s", ch.Name))
		}},
	}
	if b.adminACL == nil || b.adminACL.Channel != ch {
		return append(items, adminItem{label: "No ACL loaded for this channel"})
	}
	if b.adminTargetUser != nil && b.adminTargetUser.IsRegistered() {
		for _, permission := range commonAdminPermissions() {
			perm := permission
			items = append(items, adminItem{label: "Toggle user grant " + perm.name, action: func() {
				b.toggleACLUserGrant(b.adminTargetUser, perm.permission)
			}})
		}
	} else if b.adminTargetUser != nil {
		items = append(items, adminItem{label: "Selected user is not registered; user ACL toggles unavailable"})
	}
	items = append(items, adminItem{label: fmt.Sprintf("Inherit ACLs: %s", onOff(b.adminACL.Inherits)), action: func() {
		b.adminACL.Inherits = !b.adminACL.Inherits
		b.Client.Send(b.adminACL)
		b.AddOutputLine(fmt.Sprintf("Admin: ACL inheritance toggled for %s", b.adminACL.Channel.Name))
	}})
	for index, rule := range b.adminACL.Rules {
		items = append(items, adminItem{label: fmt.Sprintf("Rule %d: %s grant=%s deny=%s", index+1, aclSubject(rule), permissionList(rule.Granted), permissionList(rule.Denied))})
	}
	items = append(items, adminItem{label: "Advanced raw ACL command", action: func() {
		b.promptAdmin("acl grant|deny permission user ID or group NAME", func(text string) {
			b.executeAdminCommand("acl " + text)
		})
	}})
	return items
}

func (b *Barnard) adminContextActionItems() []uiterm.TreeItem {
	if b.Client == nil || len(b.Client.ContextActions) == 0 {
		return []uiterm.TreeItem{adminItem{label: "No context actions available"}}
	}
	items := []uiterm.TreeItem{}
	for _, action := range b.Client.ContextActions {
		ca := action
		label := ca.Label
		if label == "" {
			label = ca.Name
		}
		children := []uiterm.TreeItem{}
		if ca.Type&gumble.ContextActionServer != 0 {
			children = append(children, adminItem{label: "Trigger on server", action: func() {
				ca.Trigger()
				b.AddOutputLine("Admin: context action sent: " + ca.Name)
			}})
		}
		if ca.Type&gumble.ContextActionUser != 0 && b.adminTargetUser != nil {
			children = append(children, adminItem{label: "Trigger on user " + b.adminTargetUser.Name, action: func() {
				ca.TriggerUser(b.adminTargetUser)
				b.AddOutputLine("Admin: context action sent: " + ca.Name)
			}})
		}
		if ca.Type&gumble.ContextActionChannel != 0 && b.adminTargetChan != nil {
			children = append(children, adminItem{label: "Trigger on channel " + b.adminTargetChan.Name, action: func() {
				ca.TriggerChannel(b.adminTargetChan)
				b.AddOutputLine("Admin: context action sent: " + ca.Name)
			}})
		}
		items = append(items, adminItem{label: label, children: children})
	}
	return items
}

func (b *Barnard) promptAdmin(label string, action func(string)) {
	b.pendingAdminPrompt = &adminPrompt{label: label, action: action}
	b.UpdateInputStatus("[admin]")
	b.Ui.SetActive(uiViewInput)
	width, height := termboxSize()
	b.OnUiResize(b.Ui, width, height)
	b.AddOutputLine("Admin: " + label)
}

func (b *Barnard) handleAdminPrompt(text string) bool {
	if b.pendingAdminPrompt == nil {
		return false
	}
	prompt := b.pendingAdminPrompt
	b.pendingAdminPrompt = nil
	if !b.withValidAdminTargets(func() { prompt.action(strings.TrimSpace(text)) }) {
		b.AddOutputLine("Admin: action target is no longer available")
	}
	if b.Client != nil && b.Client.Self != nil {
		b.UpdateInputStatus(fmt.Sprintf("[%s]", b.Client.Self.Channel.Name))
	}
	return true
}

// withValidAdminTargets runs an action only while its selected targets are
// still members of the current connection. Menu actions can outlive server
// removal events while a prompt is open.
func (b *Barnard) withValidAdminTargets(action func()) bool {
	if b.Client == nil {
		return false
	}
	valid := true
	b.Client.Do(func() {
		if u := b.adminTargetUser; u != nil && b.Client.Users[u.Session] != u {
			valid = false
		}
		if ch := b.adminTargetChan; ch != nil && b.Client.Channels[ch.ID] != ch {
			valid = false
		}
		if valid {
			action()
		}
	})
	return valid
}

func (b *Barnard) CommandAdmin(ui *uiterm.Ui, cmd string) {
	b.executeAdminCommand(cmd)
}

func (b *Barnard) executeAdminCommand(cmd string) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 || fields[0] == "menu" {
		b.OpenAdminMenu()
		return
	}
	if b.Client == nil || b.Client.Self == nil {
		b.AddOutputLine("Admin: not connected")
		return
	}
	switch fields[0] {
	case "kick":
		user, reason := b.adminCommandUserAndReason(fields[1:])
		if user == nil {
			return
		}
		user.Kick(reason)
		b.AddOutputLine(fmt.Sprintf("Admin: kick sent for %s", user.Name))
	case "ban":
		user, reason := b.adminCommandUserAndReason(fields[1:])
		if user == nil {
			return
		}
		user.Ban(reason)
		b.AddOutputLine(fmt.Sprintf("Admin: ban sent for %s", user.Name))
	case "mute", "deafen", "suppress", "priority":
		b.executeUserToggle(fields)
	case "move":
		b.executeMoveCommand(fields)
	case "channel":
		b.executeChannelCommand(fields)
	case "banlist":
		b.Client.RequestBanList()
		b.AddOutputLine("Admin: requested ban list")
	case "unban":
		if len(fields) < 2 {
			b.AddOutputLine("Admin: usage /admin unban <index>")
			return
		}
		index, err := strconv.Atoi(fields[1])
		if err != nil {
			b.AddOutputLine("Admin: unban index must be a number")
			return
		}
		b.unbanIndex(index - 1)
	case "users":
		b.Client.RequestUserList()
		b.AddOutputLine("Admin: requested registered user list")
	case "renameuser":
		b.executeRenameRegisteredUser(fields)
	case "deregister":
		b.executeDeregisterRegisteredUser(fields)
	case "acl":
		b.executeACLCommand(fields[1:])
	case "context":
		b.executeContextCommand(fields)
	default:
		b.AddOutputLine(fmt.Sprintf("Admin: unknown command %s", fields[0]))
	}
}

func (b *Barnard) adminCommandUserAndReason(args []string) (*gumble.User, string) {
	if len(args) == 0 {
		b.AddOutputLine("Admin: user required")
		return nil, ""
	}
	user := b.findUser(args[0])
	if user == nil {
		b.AddOutputLine("Admin: user not found: " + args[0])
		return nil, ""
	}
	return user, strings.Join(args[1:], " ")
}

func (b *Barnard) executeUserToggle(fields []string) {
	if len(fields) < 2 {
		b.AddOutputLine(fmt.Sprintf("Admin: usage /admin %s <user> [on|off|toggle]", fields[0]))
		return
	}
	user := b.findUser(fields[1])
	if user == nil {
		b.AddOutputLine("Admin: user not found: " + fields[1])
		return
	}
	stateArg := "toggle"
	if len(fields) > 2 {
		stateArg = fields[2]
	}
	var current bool
	var set func(bool)
	switch fields[0] {
	case "mute":
		current, set = user.Muted, user.SetMuted
	case "deafen":
		current, set = user.Deafened, user.SetDeafened
	case "suppress":
		current, set = user.Suppressed, user.SetSuppressed
	case "priority":
		current, set = user.PrioritySpeaker, user.SetPrioritySpeaker
	}
	state, ok := parseToggleState(stateArg, current)
	if !ok {
		b.AddOutputLine("Admin: state must be on, off, or toggle")
		return
	}
	set(state)
	b.AddOutputLine(fmt.Sprintf("Admin: %s set to %s for %s", fields[0], onOff(state), user.Name))
}

func (b *Barnard) executeMoveCommand(fields []string) {
	if len(fields) < 3 {
		b.AddOutputLine("Admin: usage /admin move <user> <channel-id-or-name>")
		return
	}
	user := b.findUser(fields[1])
	channel := b.findChannel(strings.Join(fields[2:], " "))
	if user == nil {
		b.AddOutputLine("Admin: user not found: " + fields[1])
		return
	}
	if channel == nil {
		b.AddOutputLine("Admin: channel not found")
		return
	}
	user.Move(channel)
	b.AddOutputLine(fmt.Sprintf("Admin: move sent for %s to %s", user.Name, channel.Name))
}

func (b *Barnard) executeChannelCommand(fields []string) {
	if len(fields) < 3 {
		b.AddOutputLine("Admin: usage /admin channel <add|temp|rename|describe|delete|maxusers> ...")
		return
	}
	channel := b.findChannel(fields[2])
	switch fields[1] {
	case "add", "temp":
		if len(fields) < 4 {
			b.AddOutputLine("Admin: child channel name required")
			return
		}
		if channel == nil {
			b.AddOutputLine("Admin: parent channel not found")
			return
		}
		name := strings.Join(fields[3:], " ")
		channel.Add(name, fields[1] == "temp")
		b.AddOutputLine(fmt.Sprintf("Admin: create channel sent for %s", name))
	case "rename":
		if len(fields) < 4 {
			b.AddOutputLine("Admin: new channel name required")
			return
		}
		if channel == nil {
			b.AddOutputLine("Admin: channel not found")
			return
		}
		channel.SetName(strings.Join(fields[3:], " "))
		b.AddOutputLine("Admin: rename channel sent")
	case "describe":
		if channel == nil {
			b.AddOutputLine("Admin: channel not found")
			return
		}
		channel.SetDescription(strings.Join(fields[3:], " "))
		b.AddOutputLine("Admin: channel description sent")
	case "delete":
		if channel == nil {
			b.AddOutputLine("Admin: channel not found")
			return
		}
		if channel.IsRoot() {
			b.AddOutputLine("Admin: cannot delete the root channel")
			return
		}
		channel.Remove()
		b.AddOutputLine("Admin: delete channel sent")
	case "maxusers":
		if len(fields) < 4 {
			b.AddOutputLine("Admin: max users value required")
			return
		}
		if channel == nil {
			b.AddOutputLine("Admin: channel not found")
			return
		}
		maxUsers, err := strconv.ParseUint(fields[3], 10, 32)
		if err != nil {
			b.AddOutputLine("Admin: max users must be a number")
			return
		}
		channel.SetMaxUsers(uint32(maxUsers))
		b.AddOutputLine("Admin: max users sent")
	default:
		b.AddOutputLine("Admin: unknown channel command " + fields[1])
	}
}

func (b *Barnard) executeRenameRegisteredUser(fields []string) {
	if len(fields) < 3 {
		b.AddOutputLine("Admin: usage /admin renameuser <id-or-name> <new-name>")
		return
	}
	user := b.findRegisteredUser(fields[1])
	if user == nil {
		b.AddOutputLine("Admin: registered user not found")
		return
	}
	user.SetName(strings.Join(fields[2:], " "))
	b.Client.Send(b.adminUserList)
	b.AddOutputLine(fmt.Sprintf("Admin: rename registered user sent for %d", user.UserID))
}

func (b *Barnard) executeDeregisterRegisteredUser(fields []string) {
	if len(fields) < 2 {
		b.AddOutputLine("Admin: usage /admin deregister <id-or-name>")
		return
	}
	user := b.findRegisteredUser(fields[1])
	if user == nil {
		b.AddOutputLine("Admin: registered user not found")
		return
	}
	user.Deregister()
	b.Client.Send(b.adminUserList)
	b.AddOutputLine(fmt.Sprintf("Admin: deregister sent for %s", user.Name))
}

func (b *Barnard) executeACLCommand(fields []string) {
	if len(fields) == 0 || fields[0] == "request" {
		ch := b.adminTargetChan
		if len(fields) > 1 {
			ch = b.findChannel(strings.Join(fields[1:], " "))
		}
		if ch == nil {
			b.AddOutputLine("Admin: channel not found")
			return
		}
		ch.RequestACL()
		b.AddOutputLine(fmt.Sprintf("Admin: requested ACLs for %s", ch.Name))
		return
	}
	if b.adminACL == nil {
		b.AddOutputLine("Admin: request ACLs before editing")
		return
	}
	if len(fields) < 4 {
		b.AddOutputLine("Admin: usage /admin acl grant|deny <permission> user <id> | group <name>")
		return
	}
	permission, ok := parsePermission(fields[1])
	if !ok {
		b.AddOutputLine("Admin: unknown permission " + fields[1])
		return
	}
	switch fields[0] {
	case "grant", "deny":
	default:
		b.AddOutputLine("Admin: ACL command must be grant or deny")
		return
	}
	rule := b.findOrCreateACLRule(fields[2], strings.Join(fields[3:], " "))
	if rule == nil {
		return
	}
	if fields[0] == "grant" {
		rule.Granted ^= permission
		rule.Denied &^= permission
	} else {
		rule.Denied ^= permission
		rule.Granted &^= permission
	}
	b.Client.Send(b.adminACL)
	b.AddOutputLine("Admin: ACL update sent")
}

func (b *Barnard) executeContextCommand(fields []string) {
	if len(fields) < 2 {
		b.AddOutputLine("Admin: usage /admin context <action> [server|user|channel] [target]")
		return
	}
	action := b.Client.ContextActions[fields[1]]
	if action == nil {
		b.AddOutputLine("Admin: context action not found")
		return
	}
	scope := "server"
	if len(fields) > 2 {
		scope = fields[2]
	}
	switch scope {
	case "server":
		action.Trigger()
	case "user":
		if len(fields) < 4 {
			b.AddOutputLine("Admin: context user target required")
			return
		}
		user := b.findUser(fields[3])
		if user == nil {
			b.AddOutputLine("Admin: user not found")
			return
		}
		action.TriggerUser(user)
	case "channel":
		if len(fields) < 4 {
			b.AddOutputLine("Admin: context channel target required")
			return
		}
		channel := b.findChannel(strings.Join(fields[3:], " "))
		if channel == nil {
			b.AddOutputLine("Admin: channel not found")
			return
		}
		action.TriggerChannel(channel)
	default:
		b.AddOutputLine("Admin: context scope must be server, user, or channel")
		return
	}
	b.AddOutputLine("Admin: context action sent: " + action.Name)
}

func (b *Barnard) addManualBan(text string) {
	parts := strings.Fields(text)
	if len(parts) < 3 {
		b.AddOutputLine("Admin: usage ip/mask minutes reason")
		return
	}
	ip, network, err := net.ParseCIDR(parts[0])
	if err != nil {
		b.AddOutputLine("Admin: ban address must be CIDR, for example 192.0.2.1/32")
		return
	}
	duration, err := manualBanDuration(parts[1])
	if err != nil {
		b.AddOutputLine("Admin: " + err.Error())
		return
	}
	reason := strings.Join(parts[2:], " ")
	b.adminBanList.Add(ip, network.Mask, reason, duration)
	b.Client.Send(b.adminBanList)
	b.AddOutputLine("Admin: manual ban sent")
}

// manualBanDuration validates user input before it reaches the unsigned
// protocol duration field.
func manualBanDuration(text string) (time.Duration, error) {
	minutes, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ban minutes must be a number")
	}
	if minutes < 0 {
		return 0, fmt.Errorf("ban minutes must not be negative")
	}
	if minutes > int64((1<<63-1)/time.Minute) {
		return 0, fmt.Errorf("ban duration is too long")
	}
	return time.Duration(minutes) * time.Minute, nil
}

func (b *Barnard) unbanIndex(index int) {
	if index < 0 || index >= len(b.adminBanList) {
		b.AddOutputLine("Admin: ban index out of range")
		return
	}
	b.adminBanList[index].Unban()
	b.Client.Send(b.adminBanList)
	b.AddOutputLine(fmt.Sprintf("Admin: unban sent for entry %d", index+1))
}

func (b *Barnard) toggleACLUserGrant(user *gumble.User, permission gumble.Permission) {
	rule := b.findOrCreateACLRule("user", strconv.FormatUint(uint64(user.UserID), 10))
	if rule == nil {
		return
	}
	rule.Granted ^= permission
	rule.Denied &^= permission
	b.Client.Send(b.adminACL)
	b.AddOutputLine(fmt.Sprintf("Admin: ACL grant toggled for %s", user.Name))
}

func (b *Barnard) findOrCreateACLRule(subjectType, subject string) *gumble.ACLRule {
	switch subjectType {
	case "user":
		userID64, err := strconv.ParseUint(subject, 10, 32)
		if err != nil {
			b.AddOutputLine("Admin: ACL user id must be numeric")
			return nil
		}
		userID := uint32(userID64)
		for _, rule := range b.adminACL.Rules {
			if rule.User != nil && rule.User.UserID == userID {
				return rule
			}
		}
		rule := &gumble.ACLRule{
			AppliesCurrent: true,
			User:           &gumble.ACLUser{UserID: userID},
		}
		b.adminACL.Rules = append(b.adminACL.Rules, rule)
		return rule
	case "group":
		for _, rule := range b.adminACL.Rules {
			if rule.Group != nil && rule.Group.Name == subject {
				return rule
			}
		}
		group := &gumble.ACLGroup{Name: subject}
		rule := &gumble.ACLRule{
			AppliesCurrent: true,
			Group:          group,
		}
		b.adminACL.Rules = append(b.adminACL.Rules, rule)
		return rule
	default:
		b.AddOutputLine("Admin: ACL subject must be user or group")
		return nil
	}
}

func (b *Barnard) findUser(token string) *gumble.User {
	if b.Client == nil {
		return nil
	}
	if session, err := strconv.ParseUint(token, 10, 32); err == nil {
		if user := b.Client.Users[uint32(session)]; user != nil {
			return user
		}
	}
	for _, user := range b.Client.Users {
		if strings.EqualFold(user.Name, token) {
			return user
		}
	}
	return nil
}

func (b *Barnard) findChannel(token string) *gumble.Channel {
	if b.Client == nil {
		return nil
	}
	token = strings.TrimSpace(token)
	if id, err := strconv.ParseUint(token, 10, 32); err == nil {
		if channel := b.Client.Channels[uint32(id)]; channel != nil {
			return channel
		}
	}
	for _, channel := range b.Client.Channels {
		if strings.EqualFold(channel.Name, token) {
			return channel
		}
	}
	return nil
}

func (b *Barnard) findRegisteredUser(token string) *gumble.RegisteredUser {
	if id, err := strconv.ParseUint(token, 10, 32); err == nil {
		for _, user := range b.adminUserList {
			if user.UserID == uint32(id) {
				return user
			}
		}
	}
	for _, user := range b.adminUserList {
		if strings.EqualFold(user.Name, token) {
			return user
		}
	}
	return nil
}

func (b *Barnard) adminCanRoot(permission gumble.Permission) bool {
	if b.Client == nil {
		return true
	}
	return b.adminCanChannel(b.Client.Channels[0], permission)
}

func (b *Barnard) adminCanChannel(channel *gumble.Channel, permission gumble.Permission) bool {
	if channel == nil {
		return true
	}
	current := channel.Permission()
	if current == nil {
		return true
	}
	return current.Has(permission)
}

func parseToggleState(text string, current bool) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "toggle":
		return !current, true
	case "on", "true", "yes", "1":
		return true, true
	case "off", "false", "no", "0":
		return false, true
	default:
		return false, false
	}
}

type permissionOption struct {
	name       string
	permission gumble.Permission
}

func commonAdminPermissions() []permissionOption {
	return []permissionOption{
		{"write", gumble.PermissionWrite},
		{"enter", gumble.PermissionEnter},
		{"speak", gumble.PermissionSpeak},
		{"mute_deafen", gumble.PermissionMuteDeafen},
		{"move", gumble.PermissionMove},
		{"kick", gumble.PermissionKick},
		{"ban", gumble.PermissionBan},
	}
}

func parsePermission(text string) (gumble.Permission, bool) {
	for _, option := range commonAdminPermissions() {
		if option.name == text {
			return option.permission, true
		}
	}
	switch text {
	case "traverse":
		return gumble.PermissionTraverse, true
	case "make_channel":
		return gumble.PermissionMakeChannel, true
	case "link_channel":
		return gumble.PermissionLinkChannel, true
	case "whisper":
		return gumble.PermissionWhisper, true
	case "text_message":
		return gumble.PermissionTextMessage, true
	case "make_temp_channel":
		return gumble.PermissionMakeTemporaryChannel, true
	case "register":
		return gumble.PermissionRegister, true
	case "register_self":
		return gumble.PermissionRegisterSelf, true
	default:
		return 0, false
	}
}

func permissionList(permission gumble.Permission) string {
	if permission == 0 {
		return "none"
	}
	names := []string{}
	for _, option := range append(commonAdminPermissions(), []permissionOption{
		{"traverse", gumble.PermissionTraverse},
		{"make_channel", gumble.PermissionMakeChannel},
		{"link_channel", gumble.PermissionLinkChannel},
		{"whisper", gumble.PermissionWhisper},
		{"text_message", gumble.PermissionTextMessage},
		{"make_temp_channel", gumble.PermissionMakeTemporaryChannel},
		{"register", gumble.PermissionRegister},
		{"register_self", gumble.PermissionRegisterSelf},
	}...) {
		if permission.Has(option.permission) {
			names = append(names, option.name)
		}
	}
	return strings.Join(names, ",")
}

func aclSubject(rule *gumble.ACLRule) string {
	if rule.User != nil {
		if rule.User.Name != "" {
			return "user " + rule.User.Name
		}
		return fmt.Sprintf("user %d", rule.User.UserID)
	}
	if rule.Group != nil {
		return "group " + rule.Group.Name
	}
	return "all"
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}
