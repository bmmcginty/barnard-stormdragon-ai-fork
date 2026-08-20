package main

import (
	"fmt"
	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/gumble/gumbleopenal"
	"git.stormux.org/storm/barnard/uiterm"
	"sort"
)

func (ti TreeItem) String() string {
	if ti.display != "" {
		return ti.display
	}
	if ti.User != nil {
		if ti.User.LocallyMuted() {
			return "[MUTED] " + esc(ti.User.Name)
		}
		// Calculate total volume as percentage
		boostPercent := float32(ti.User.Boost()-1) * 10
		totalVolume := ti.User.Volume()*100 + boostPercent
		return fmt.Sprintf("%s [%.0f%%]", esc(ti.User.Name), totalVolume)
	}
	if ti.Channel != nil {
		return "#" + esc(ti.Channel.Name)
	}
	return ""
}

func (ti TreeItem) TreeItemStyle(fg, bg uiterm.Attribute, active bool) (uiterm.Attribute, uiterm.Attribute) {
	if ti.Channel != nil {
		fg |= uiterm.AttrBold
	}
	if active {
		fg, bg = bg, fg
	}
	return fg, bg
}

func (b *Barnard) changeVolume(users []*gumble.User, change float32) {
	changed := b.withStream(func(stream *gumbleopenal.Stream) {
		for _, u := range users {
			var boost uint16
			var ng float32
			curboost := float32((u.Boost() - 1)) / 10
			ng = u.Volume() + curboost + change
			boost = uint16(1)
			if ng > 1.0 {
				perc := uint16((ng * 10)) - 10
				perc += 1
				boost = perc
				ng = 1.0
			}
			if ng < 0 {
				ng = 0.0
			}
			u.SetBoost(boost)
			u.SetVolume(ng)
			stream.UpdateUserGain(u)
			b.UserConfig.UpdateConfig(u)
		}
		if err := b.UserConfig.SaveConfig(); err != nil {
			b.AddOutputLine("Volume: could not save setting: " + err.Error())
		}
	})
	if changed {
		b.refreshVolumeDisplay()
	}
}

func (b *Barnard) resetVolume(users []*gumble.User) {
	changed := b.withStream(func(stream *gumbleopenal.Stream) {
		for _, u := range users {
			// Reset to original volume (1.0) and boost (1)
			u.SetBoost(uint16(1))
			u.SetVolume(1.0)
			stream.UpdateUserGain(u)
			b.UserConfig.UpdateConfig(u)
		}
		if err := b.UserConfig.SaveConfig(); err != nil {
			b.AddOutputLine("Volume: could not save setting: " + err.Error())
		}
	})
	if changed {
		b.refreshVolumeDisplay()
	}
}

// Tree items render a display string snapshotted at build time, so a volume
// change is only visible after the tree is rebuilt.
func (b *Barnard) refreshVolumeDisplay() {
	b.RebuildUserChannelTreePreservingSelection()
	b.Ui.Refresh()
}

func makeUsersArray(users gumble.Users) []*gumble.User {
	t := make([]*gumble.User, 0, len(users))
	for _, u := range users {
		t = append(t, u)
	}
	return t
}

func (b *Barnard) TreeItemBuild(item uiterm.TreeItem) []uiterm.TreeItem {
	if b.Client == nil {
		return nil
	}

	var treeItem TreeItem
	if ti, ok := item.(TreeItem); !ok {
		var root *gumble.Channel
		b.Client.Do(func() { root = b.Client.Channels[0] })
		if root == nil {
			return nil
		}
		var display string
		var channelID uint32
		b.Client.Do(func() {
			display = "#" + esc(root.Name)
			channelID = root.ID
		})
		return []uiterm.TreeItem{TreeItem{Channel: root, display: display, channelID: channelID, snapshot: true}}
	} else {
		treeItem = ti
	}

	if treeItem.User != nil {
		return nil
	}

	users := []uiterm.TreeItem{}
	type userDisplay struct {
		user    *gumble.User
		display string
		name    string
		session uint32
	}
	type channelDisplay struct {
		channel *gumble.Channel
		name    string
		id      uint32
	}
	ul := []userDisplay{}
	cl := []channelDisplay{}
	// TCP handlers mutate both maps; snapshot them while Client.Do holds its
	// read lock, then sort/render outside the protocol critical section.
	b.Client.Do(func() {
		for _, user := range treeItem.Channel.Users {
			boostPercent := float32(user.Boost()-1) * 10
			totalVolume := user.Volume()*100 + boostPercent
			display := fmt.Sprintf("%s [%.0f%%]", esc(user.Name), totalVolume)
			if user.LocallyMuted() {
				display = "[MUTED] " + display
			}
			ul = append(ul, userDisplay{user: user, name: user.Name, session: user.Session, display: display})
		}
		for _, subchannel := range treeItem.Channel.Children {
			cl = append(cl, channelDisplay{channel: subchannel, name: subchannel.Name, id: subchannel.ID})
		}
	})
	sort.Slice(ul, func(i, j int) bool {
		return ul[i].name < ul[j].name
	})
	for _, user := range ul {
		users = append(users, TreeItem{User: user.user, display: user.display, userSession: user.session, snapshot: true})
	}

	channels := []uiterm.TreeItem{}
	sort.Slice(cl, func(i, j int) bool {
		return cl[i].name < cl[j].name
	})
	for _, subchannel := range cl {
		displayName := "#" + esc(subchannel.name)
		if b.isChannelMuted(subchannel.id) {
			displayName = "[MUTED] " + displayName
		}
		channels = append(channels, TreeItem{Channel: subchannel.channel, display: displayName, channelID: subchannel.id, snapshot: true})
	}

	return append(users, channels...)
}

func (b *Barnard) RebuildUserChannelTreePreservingSelection() {
	b.UiTree.RebuildPreservingActiveItem(sameUserChannelTreeItem)
}

func sameUserChannelTreeItem(previous, current uiterm.TreeItem) bool {
	prev, ok := previous.(TreeItem)
	if !ok {
		return false
	}
	cur, ok := current.(TreeItem)
	if !ok {
		return false
	}
	if prev.User != nil && cur.User != nil {
		if prev.snapshot && cur.snapshot {
			return prev.userSession == cur.userSession
		}
		return prev.User.Session == cur.User.Session
	}
	if prev.Channel != nil && cur.Channel != nil {
		if prev.snapshot && cur.snapshot {
			return prev.channelID == cur.channelID
		}
		return prev.Channel.ID == cur.Channel.ID
	}
	return false
}
