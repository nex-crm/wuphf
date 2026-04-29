package channelui

import "github.com/nex-crm/wuphf/internal/company"

// MergeOfficeMembers returns all current channel members enriched with
// office-roster metadata and broker activity. The result preserves
// channel.Members order when channel is non-nil and lists at least one
// member; otherwise it follows officeMembers order. Broker entries
// whose slugs are not part of the chosen order are appended at the end
// so we never silently drop anyone who has posted. Members who haven't
// posted yet still appear and are filled in from the office roster
// (Name / Role) plus a fallback to DisplayName / RoleLabel.
func MergeOfficeMembers(officeMembers []OfficeMember, brokerMembers []Member, channel *ChannelInfo) []Member {
	memberOrder := make([]string, 0)
	if channel != nil && len(channel.Members) > 0 {
		memberOrder = append(memberOrder, channel.Members...)
	} else {
		for _, member := range officeMembers {
			memberOrder = append(memberOrder, member.Slug)
		}
	}

	officeMap := make(map[string]OfficeMember, len(officeMembers))
	for _, member := range officeMembers {
		officeMap[member.Slug] = member
	}
	brokerMap := make(map[string]Member, len(brokerMembers))
	for _, member := range brokerMembers {
		brokerMap[member.Slug] = member
	}

	result := make([]Member, 0, len(memberOrder))
	for _, slug := range memberOrder {
		member := brokerMap[slug]
		member.Slug = slug
		if meta, ok := officeMap[slug]; ok {
			if member.Name == "" {
				member.Name = meta.Name
			}
			if member.Role == "" {
				member.Role = meta.Role
			}
		}
		if member.Name == "" {
			member.Name = DisplayName(slug)
		}
		if member.Role == "" {
			member.Role = RoleLabel(slug)
		}
		result = append(result, member)
	}
	for _, member := range brokerMembers {
		if ContainsSlug(memberOrder, member.Slug) {
			continue
		}
		result = append(result, member)
	}
	return result
}

// OfficeMembersFromManifest projects company.Manifest.Members into the
// OfficeMember type used by the channel UI. Expertise slices are
// defensively copied so later mutation of the manifest does not leak
// into UI state.
func OfficeMembersFromManifest(manifest company.Manifest) []OfficeMember {
	members := make([]OfficeMember, 0, len(manifest.Members))
	for _, member := range manifest.Members {
		members = append(members, OfficeMember{
			Slug:        member.Slug,
			Name:        member.Name,
			Role:        member.Role,
			Expertise:   append([]string(nil), member.Expertise...),
			Personality: member.Personality,
			BuiltIn:     member.System,
		})
	}
	return members
}

// ChannelInfosFromManifest projects company.Manifest.Channels into the
// ChannelInfo type. Members and Disabled slices are defensively copied.
func ChannelInfosFromManifest(manifest company.Manifest) []ChannelInfo {
	channels := make([]ChannelInfo, 0, len(manifest.Channels))
	for _, channel := range manifest.Channels {
		channels = append(channels, ChannelInfo{
			Slug:     channel.Slug,
			Name:     channel.Name,
			Members:  append([]string(nil), channel.Members...),
			Disabled: append([]string(nil), channel.Disabled...),
		})
	}
	return channels
}

// OfficeMembersFallback returns existing untouched if it has any
// entries, otherwise loads the manifest from disk (falling back to the
// default manifest on error) and projects its members. Used when the
// broker hasn't reported a roster yet so the UI has someone to show.
func OfficeMembersFallback(existing []OfficeMember) []OfficeMember {
	if len(existing) > 0 {
		return existing
	}
	manifest, err := company.LoadManifest()
	if err != nil {
		manifest = company.DefaultManifest()
	}
	return OfficeMembersFromManifest(manifest)
}

// ChannelInfosFallback returns existing untouched if it has any
// entries, otherwise loads the manifest from disk (falling back to the
// default manifest on error) and projects its channels.
func ChannelInfosFallback(existing []ChannelInfo) []ChannelInfo {
	if len(existing) > 0 {
		return existing
	}
	manifest, err := company.LoadManifest()
	if err != nil {
		manifest = company.DefaultManifest()
	}
	return ChannelInfosFromManifest(manifest)
}
