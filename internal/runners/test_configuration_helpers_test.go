package runners


// modelRuntimeLimitsStub provides test defaults matching the hardcoded
// constants used by truncateOldToolResults. This is a temporary stub until
// the ModelRuntimeLimits type lands in models.
type modelRuntimeLimitsStub struct {
	MinKeepMessages    int
	MaxToolResultChars int
}

func defaultModelRuntimeLimits() modelRuntimeLimitsStub {
	return modelRuntimeLimitsStub{
		MinKeepMessages:    10,
		MaxToolResultChars: 8000,
	}
}
