// Command basic 是 flowlite 的最小可运行示例：内存存储、一个两级审批、走完一个实例。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/nexadmin/flowlite/engine"
	"github.com/nexadmin/flowlite/runtime"
	"github.com/nexadmin/flowlite/store"
	"github.com/nexadmin/flowlite/store/memory"
)

func main() {
	ctx := context.Background()
	b := memory.New()
	svc := runtime.New(b.Defs, b.Insts, b.Tasks, b.Actions)
	svc.OnEvent = func(_ context.Context, ev runtime.Event) {
		fmt.Printf("[event] %-9s %-8s approvers=%v\n", ev.Name, ev.Inst.Title, ev.Approvers)
	}

	def := engine.Definition{
		Key: "leave", Name: "Leave approval", Start: "start",
		Nodes: []engine.Node{
			{Key: "start", Kind: engine.KindStart},
			{Key: "manager", Name: "Manager", Kind: engine.KindApproval, Approvers: []string{"alice"}},
			{Key: "hr", Name: "HR", Kind: engine.KindApproval, Approvers: []string{"bob", "carol"}, Mode: engine.ModeAll},
			{Key: "end", Kind: engine.KindEnd},
		},
		Edges: []engine.Edge{
			{From: "start", To: "manager"},
			{From: "manager", To: "hr"},
			{From: "hr", To: "end"},
		},
	}
	raw, _ := json.Marshal(def)
	if _, err := svc.Defs.Save(ctx, &store.Definition{
		Key: def.Key, Name: def.Name, Status: store.DefPublished, Definition: raw,
	}); err != nil {
		log.Fatal(err)
	}

	res, err := svc.Start(ctx, runtime.StartInput{DefKey: def.Key, Title: "Alice / 3 days"}, "alice")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("started %s at %v\n", res.Inst.ID, res.State.Current)

	for _, actor := range []string{"alice", "bob", "carol"} {
		res, err = svc.Act(ctx, res.Inst.ID, actor, runtime.ActionApprove, runtime.ActionInput{Comment: "ok"})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%-6s approved → status=%s current=%v\n", actor, res.State.Status, res.State.Current)
	}

	xml, err := engine.ExportXML(def)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nBPMN 2.0 export (excerpt):")
	fmt.Println(xml[:min(len(xml), 320)])
}
