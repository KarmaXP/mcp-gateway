package policy

func ReloadEngine(holder *Holder, in EngineInput) {
	if holder == nil {
		return
	}
	holder.Store(NewEngine(in))
}
