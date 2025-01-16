package main

import (
    "git.stormux.org/storm/barnard/gumble/gumble"
    "git.stormux.org/storm/barnard/uiterm"
    "sort"
)

type TreeItem struct {
    User    *gumble.User
    Channel *gumble.Channel
}

func (ti TreeItem) String() string {
    if ti.User != nil {
        if ti.User.LocallyMuted {
            return "[MUTED] " + ti.User.Name
        }
        return ti.User.Name
    }
    if ti.Channel != nil {
        return "#" + ti.Channel.Name
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

func (b *Barnard) TreeItemCharacter(ui *uiterm.Ui, tree *uiterm.Tree, item uiterm.TreeItem, ch rune) {
}

func (b *Barnard) changeVolume(users []*gumble.User, change float32) {
    for _, u := range users {
        au := u.AudioSource
        if au == nil {
            continue
        }
        var boost uint16
        var cv float32
        var ng float32
        var curboost float32
        curboost = float32((u.Boost - 1)) / 10
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
        u.Boost = boost
        u.Volume = ng
        if !u.LocallyMuted {
            au.SetGain(ng)
        }
        b.UserConfig.UpdateConfig(u)
    }
    b.UserConfig.SaveConfig()
}

func makeUsersArray(users gumble.Users) []*gumble.User {
    t := make([]*gumble.User, 0, len(users))
    for _, u := range users {
        t = append(t, u)
    }
    return t
}

func (b *Barnard) TreeItemKeyPress(ui *uiterm.Ui, tree *uiterm.Tree, item uiterm.TreeItem, key uiterm.Key) {
    treeItem := item.(TreeItem)
    if key == uiterm.KeyEnter {
        if treeItem.Channel != nil {
            b.Client.Self.Move(treeItem.Channel)
            b.SetSelectedUser(nil)
            b.GotoChat()
        }
        if treeItem.User != nil {
            if b.selectedUser == treeItem.User {
                b.SetSelectedUser(nil)
                b.GotoChat()
            } else {
                b.SetSelectedUser(treeItem.User)
                b.GotoChat()
            }
        }
    }

    // Handle mute toggle
    if treeItem.Channel != nil {
        if key == *b.Hotkeys.MuteToggle {
            // Toggle mute for all users in channel
            users := makeUsersArray(treeItem.Channel.Users)
            for _, u := range users {
                b.UserConfig.ToggleMute(u)
                if u.AudioSource != nil {
                    if u.LocallyMuted {
                        u.AudioSource.SetGain(0)
                    } else {
                        u.AudioSource.SetGain(u.Volume)
                    }
                }
            }
            b.UiTree.Rebuild()
            b.Ui.Refresh()
        }
        if key == *b.Hotkeys.VolumeDown {
            b.changeVolume(makeUsersArray(treeItem.Channel.Users), -0.1)
        }
        if key == *b.Hotkeys.VolumeUp {
            b.changeVolume(makeUsersArray(treeItem.Channel.Users), 0.1)
        }
    }

    if treeItem.User != nil {
        if key == *b.Hotkeys.MuteToggle {
            // Toggle mute for single user
            b.UserConfig.ToggleMute(treeItem.User)
            if treeItem.User.AudioSource != nil {
                if treeItem.User.LocallyMuted {
                    treeItem.User.AudioSource.SetGain(0)
                } else {
                    treeItem.User.AudioSource.SetGain(treeItem.User.Volume)
                }
            }
            b.UiTree.Rebuild()
            b.Ui.Refresh()
        }
        if key == *b.Hotkeys.VolumeDown {
            b.changeVolume([]*gumble.User{treeItem.User}, -0.1)
        }
        if key == *b.Hotkeys.VolumeUp {
            b.changeVolume([]*gumble.User{treeItem.User}, 0.1)
        }
    }
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
        channels = append(channels, TreeItem{
            Channel: subchannel,
        })
    }

    return append(users, channels...)
}
