package asc

import "time"

// Summary is the TestFlight block on /admin. Zero optional pointers mean
// that piece was not returned; the page says so instead of inventing numbers.
type Summary struct {
	Testers     int
	Installed   int
	Accepted    int
	Invited     int
	OtherSt     int
	Builds      []BuildCount
	LatestBuild string
	Groups      []Group
	Usage7      *Usage
	Usage30     *Usage
	Devices     []NameCount
	OSVersions  []NameCount
	DeviceTotal int
	Review      *Review
	FetchedAt   time.Time
}

type BuildCount struct {
	Build   string
	Version string
	Testers int
}

type Group struct {
	Name        string
	Testers     int
	Internal    bool
	PublicLink  bool
	LimitOn     bool
	Limit       int
	LinkViews   int
	LinkJoined  int
	LinkMetrics bool
}

type Usage struct {
	Sessions      int
	Crashes       int
	Feedback      int
	ActiveTesters int
}

type NameCount struct {
	Name  string
	Count int
}

type Review struct {
	Version string
	Build   string
	State   string
}

// StateName is the Chinese label for an App Store review state.
func StateName(state string) string {
	switch state {
	case "WAITING_FOR_REVIEW":
		return "等待审核"
	case "IN_REVIEW":
		return "审核中"
	case "READY_FOR_REVIEW":
		return "可提交审核"
	case "PENDING_DEVELOPER_RELEASE":
		return "等待开发者发布"
	case "PENDING_APPLE_RELEASE":
		return "等待苹果发布"
	case "READY_FOR_SALE", "READY_FOR_DISTRIBUTION":
		return "已上架"
	case "REJECTED":
		return "被拒绝"
	case "METADATA_REJECTED":
		return "元数据被拒"
	case "DEVELOPER_REJECTED":
		return "开发者撤回"
	case "PREPARE_FOR_SUBMISSION":
		return "准备提交"
	case "PROCESSING_FOR_APP_STORE":
		return "处理中"
	case "":
		return "—"
	default:
		return state
	}
}
