// Offline probes against the real runtime. Run from runtime/:
// go run ../docs/audit/2026-09-08/evidence/check_runtime.go
// An optional "race" argument exercises concurrent graph submissions.
package main

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "sync"
 "time"
 "github.com/reticle/runtime/agent"
 "github.com/reticle/runtime/events"
 "github.com/reticle/runtime/logger"
 "gopkg.in/yaml.v3"
)

func main() {
 if len(os.Args)>1 && os.Args[1]=="child" {
  switch os.Args[2] {
  case "eof": io.ReadAll(os.Stdin)
  case "optional": var req map[string]any; json.NewDecoder(os.Stdin).Decode(&req); fmt.Println(`{"id":"audit","result":"valid legacy result"}`); return
  default: var req map[string]any; json.NewDecoder(os.Stdin).Decode(&req)
  }
  fmt.Println(`{"id":"wrong-id","artifact":{"id":"synthetic","data":"synthetic"}}`)
  return
 }
 root,_ := filepath.Abs("..")
 if len(os.Args)>1 && os.Args[1]=="race" {
  engine:=agent.NewGraphEngine(&logger.Logger{},events.NewBus("audit"))
  var wg sync.WaitGroup
  for n:=0;n<4;n++ {wg.Add(1);go func(n int){defer wg.Done();for i:=0;i<30;i++ {engine.SubmitWorkflow(&agent.WorkflowDefinition{ID:"fixture",Nodes:map[string]agent.WorkflowNode{}},fmt.Sprintf("audit-%d-%d",n,i))}}(n)}
  wg.Wait();return
 }
 result:=map[string]any{}
 registry:=agent.NewRegistry()
 err:=registry.LoadAgents(filepath.Join(root,"cmd/forge/compiler/agents"))
 if err!=nil {result["agent_load_error"]=err.Error()}
 ids:=[]string{};for id:=range registry.Definitions {ids=append(ids,string(id))}
 result["registered_agents"]=ids
 result["ml_registered"]=registry.Definitions["ml-agent"].ID!=""
 result["devops_registered"]=registry.Definitions["devops-agent"].ID!=""
 data,_:=os.ReadFile(filepath.Join(root,"cmd/forge/compiler/agents/ml-agent/definition.yml"))
 var def agent.AgentDefinition
 if err:=yaml.Unmarshal(data,&def);err!=nil {result["ml_definition_if_renamed"]=err.Error()}
 if err:=registry.LoadWorkflows(filepath.Join(root,"examples/06_ml_training_pipeline/workflows"));err!=nil {result["ml_workflow_error"]=err.Error()}
 executable,_:=os.Executable()
 for _,mode:=range []string{"optional","eof","wrong-id"} {
  w:=agent.NewWorker("audit",executable,[]string{"child",mode},nil,&logger.Logger{},events.NewBus("audit"))
  ctx,cancel:=context.WithTimeout(context.Background(),500*time.Millisecond)
  response,failure:=w.Execute(ctx,agent.Task{ID:"audit"})
  result["worker_"+mode]=map[string]any{"response":response,"failure":failure,"deadline_reached":ctx.Err()!=nil}
  cancel()
 }
 wf:=&agent.WorkflowDefinition{ID:"fixture",Nodes:map[string]agent.WorkflowNode{"one":{ID:"one",Parameters:map[string]any{"effort":"high"}}}}
 a,b:=agent.NewWorkflowExecution("a",wf),agent.NewWorkflowExecution("b",wf)
 a.Workflow.Nodes["injected"]=agent.WorkflowNode{ID:"injected"}
 _,shared:=b.Workflow.Nodes["injected"]
 result["workflow_definition_shared_between_executions"]=shared
 bus:=events.NewBus("audit");sm:=agent.NewSubscriptionManager(&logger.Logger{},bus)
 triggered:=make(chan bool,1)
 bus.Subscribe("TaskCreated",func(events.RuntimeEvent){triggered<-true})
 sm.Register(&agent.Subscription{ID:"remove-me",WorkerID:"fixture",EventType:"AuditTrigger"})
 sm.Remove("remove-me");bus.Publish("AuditTrigger","audit",nil)
 select {case <-triggered:result["removed_subscription_still_triggers"]=true;case <-time.After(time.Second):result["removed_subscription_still_triggers"]=false}
 out,_:=json.MarshalIndent(result,"","  ")
 os.WriteFile(filepath.Join(root,"docs/audit/2026-09-08/evidence/runtime-results.json"),out,0644)
 fmt.Println(string(out))
}
