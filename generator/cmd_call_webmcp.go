package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"sightkick/generator/internal/gen"
)

// probeJS reports what WebMCP surface the page offers, if any.
//
// `__sightkick` is the runtime's own entry point, present when the runtime
// booted itself into a global. `modelContext` is the WebMCP surface proper,
// which is what a real client talks to — a page that boots the runtime
// privately populates it without exposing the global.
const probeJS = `(function(){
  if (window.__sightkick && window.__sightkick.call) return "sightkick";
  if (document.modelContext && document.modelContext.executeTool) return "modelcontext";
  return "none";
})()`

// callJS asks the page to run a tool and stashes the result in a global,
// because the eval that starts it cannot await the promise it returns.
//
// It prefers the runtime's own entry point, which takes a plain name and
// resolves to a plain result. Failing that it goes through the WebMCP surface,
// which differs in three ways between Chrome's built-in modelContext and the
// runtime's polyfill: the built-in rejects a bare {name} and wants the whole
// registered tool that getTools() handed out; it takes arguments as a JSON
// string where the polyfill takes an object; and it resolves to a JSON string
// of the result envelope where the polyfill resolves to the envelope itself.
// The polyfill marks itself, so the argument shape is chosen from that, and
// the result is unwrapped by inspecting what came back. Either way the tool's
// own result arrives as text content inside the envelope.
const callJS = `window.%[1]s = null;
(function(name, args){
  var done = function(r){ window.%[1]s = JSON.stringify(r); };
  var fail = function(e){ window.%[1]s = JSON.stringify({ok:false, message:String((e && e.message) || e)}); };
  try {
    if (window.__sightkick && window.__sightkick.call) {
      window.__sightkick.call(name, args).then(done, fail);
      return;
    }
    var ctx = document.modelContext;
    Promise.resolve(ctx.getTools()).then(function(tools){
      var registered = tools || [];
      var tool = registered.filter(function(t){ return t.name === name; })[0];
      if (!tool) {
        // No tools at all means the runtime isn't on the page; some tools but
        // not this one means it is, and this tool belongs to another view.
        // Different problems, so say which one it is.
        if (registered.length === 0) {
          fail("no WebMCP tools are registered on this page. If it does not embed the sightkick " +
               "runtime, either run 'sightkick browser <app-dir>' to inject it, or pass --via cli " +
               "to drive the tool through the sightmap CLI instead (real input events, no runtime).");
        } else {
          fail('tool "' + name + '" is not among the ' + registered.length + " registered here (" +
               registered.map(function(t){ return t.name; }).join(", ") + "). A tool is only " +
               "offered on its own view, so check the page is the one it belongs to.");
        }
        return;
      }
      var payload = ctx.__sightkickPolyfill ? args : JSON.stringify(args);
      return Promise.resolve(ctx.executeTool(tool, payload)).then(function(result){
        try {
          var envelope = (typeof result === "string") ? JSON.parse(result) : result;
          done(JSON.parse(envelope.content[0].text));
        } catch (e) {
          fail("unexpected tool result: " + JSON.stringify(result));
        }
      });
    }).catch(fail);
  } catch (e) { fail(e); }
})(%[2]s, %[3]s);
"started"`

// runWebMCP runs a tool by asking the page's registered WebMCP tool to run
// itself, and returns the result it reports.
//
// Nothing here interprets the tool's steps: the page holds the compiled tool
// and executes it. That makes this the path a real WebMCP client takes, and
// the reason to prefer it — it exercises the contract the runtime actually
// ships. Its cost is that the tool has to be registered on the page first, and
// that the runtime clicks by dispatching synthetic events, which do not reach
// elements rendered into a portal.
func (s *session) runWebMCP(toolName string, args map[string]any, timeoutMs int) (toolOutcome, error) {
	surface, err := s.evalValue(probeJS)
	if err != nil {
		return toolOutcome{}, fmt.Errorf("probe the page for WebMCP tools: %w", err)
	}
	if surface == "none" {
		return toolOutcome{}, errors.New(
			"no WebMCP tools are registered on this page.\n" +
				"       Either serve a page that boots the sightkick runtime, or pass --via cli to\n" +
				"       drive the tool through the sightmap CLI instead (real input events, no runtime).")
	}

	nameJSON, err := json.Marshal(toolName)
	if err != nil {
		return toolOutcome{}, err
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return toolOutcome{}, err
	}

	// A fresh global per invocation, so two overlapping calls — or a leftover
	// from one that was killed — cannot read each other's result.
	slot := fmt.Sprintf("__sightkick_call_%d", time.Now().UnixNano())
	if _, err := s.evalValue(fmt.Sprintf(callJS, slot, nameJSON, argsJSON)); err != nil {
		return toolOutcome{}, fmt.Errorf("start tool %q: %w", toolName, err)
	}

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		stashed, err := s.evalValue("window." + slot)
		if err != nil {
			return toolOutcome{}, fmt.Errorf("read the result of tool %q: %w", toolName, err)
		}
		if stashed != "" {
			return parseWebMCPResult(stashed)
		}
		if time.Now().After(deadline) {
			return toolOutcome{}, fmt.Errorf("tool %q did not finish within %dms", toolName, timeoutMs)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// webMCPResult is the result shape the runtime reports. items and value are
// pointers so an absent one stays distinguishable from an empty one.
type webMCPResult struct {
	OK       bool                 `json:"ok"`
	Value    *string              `json:"value"`
	Items    *[]map[string]string `json:"items"`
	Message  string               `json:"message"`
	Skipped  bool                 `json:"skipped"`
	Guidance []gen.Suggestion     `json:"guidance"`
}

func parseWebMCPResult(s string) (toolOutcome, error) {
	var res webMCPResult
	if err := json.Unmarshal([]byte(s), &res); err != nil {
		return toolOutcome{}, fmt.Errorf("unexpected tool result %q: %w", s, err)
	}
	out := toolOutcome{
		ok:       res.OK,
		message:  res.Message,
		skipped:  res.Skipped,
		value:    res.Value,
		guidance: res.Guidance,
	}
	if res.Items != nil {
		out.items = *res.Items
	}
	return out, nil
}
