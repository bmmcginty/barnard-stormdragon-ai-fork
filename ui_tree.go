package main

import (
	"fmt"
	"git.stormux.org/storm/barnard/gumble/gumble"
	"git.stormux.org/storm/barnard/uiterm"
	"sort"
)

func (ti TreeItem) String() string {
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
	for _, u := range users {
		au := u.AudioSource()
		if au == nil {
			continue
		}
		var boost uint16
		var cv float32
		var ng float32
		var curboost float32
		curboost = float32((u.Boost() - 1)) / 10
		cv = au.GetGain() + curboost
		ng = cv + change
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
		if !u.LocallyMuted() {
			au.SetGain(ng)
		}
		b.UserConfig.UpdateConfig(u)
	}
	if err := b.UserConfig.SaveConfig(); err != nil {
		b.AddOutputLine("Volume: could not save setting: " + err.Error())
	}
}

func (b *Barnard) resetVolume(users []*gumble.User) {
	for _, u := range users {
		au := u.AudioSource()
		if au == nil {
			continue
		}
		// Reset to original volume (1.0) and boost (1)
		u.SetBoost(uint16(1))
		u.SetVolume(1.0)
		if !u.LocallyMuted() {
			au.SetGain(1.0)
		}
		b.UserConfig.UpdateConfig(u)
	}
	if err := b.UserConfig.SaveConfig(); err != nil {
		b.AddOutputLine("Volume: could not save setting: " + err.Error())
	}
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
		root := b.Client.Channels[0]
		if root == nil {
			return nil
		}
		return []uiterm.TreeItem{
			TreeItem{
				Channel: root,
			},
		}
	} else {
		treeItem = ti
	}

	if treeItem.User != nil {
		return nil
	}

	users := []uiterm.TreeItem{}
	ul := []*gumble.User{}
	for _, user := range treeItem.Channel.Users {
		ul = append(ul, user)
		var u = ul[len(ul)-1]
		_ = u
	}
	sort.Slice(ul, func(i, j int) bool {
		return ul[i].Name < ul[j].Name
	})
	for _, user := range ul {
		users = append(users, TreeItem{
			User: user,
		})
	}

	channels := []uiterm.TreeItem{}
	cl := []*gumble.Channel{}
	for _, subchannel := range treeItem.Channel.Children {
		cl = append(cl, subchannel)
	}
	sort.Slice(cl, func(i, j int) bool {
		return cl[i].Name < cl[j].Name
	})
	for _, subchannel := range cl {
		displayName := subchannel.Name
		if b.MutedChannels[subchannel.ID] {
			displayName = "[MUTED] #" + displayName
		} else {
			displayName = "#" + displayName
		}
		channels = append(channels, TreeItem{
			Channel: subchannel,
		})
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
		return prev.User.Session == cur.User.Session
	}
	if prev.Channel != nil && cur.Channel != nil {
		return prev.Channel.ID == cur.Channel.ID
	}
	return false
}
