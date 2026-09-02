import { useState } from 'react';
import { Panel, Group as PanelGroup, Separator as PanelResizeHandle } from 'react-resizable-panels';
import { Send, Terminal, Code2, Play, Layout, Settings, Database, Server } from 'lucide-react';
import GraphView from './GraphView';
import TitleBar from './components/TitleBar';

function App() {
  const [prompt, setPrompt] = useState('');

  return (
    <div className="flex flex-col h-screen w-screen bg-[#020617] text-slate-200 overflow-hidden font-sans">
      <TitleBar />
      
      <div className="flex flex-1 overflow-hidden">
        {/* Activity Bar (VSCode Style) */}
        <div className="w-14 bg-slate-950 border-r border-slate-800 flex flex-col items-center py-4 gap-6 flex-shrink-0 z-10">
          <div className="w-10 h-10 rounded-xl bg-emerald-500/10 text-emerald-400 flex items-center justify-center cursor-pointer hover:bg-emerald-500/20 transition-colors shadow-[0_0_15px_rgba(16,185,129,0.15)]">
            <Layout className="w-5 h-5" />
          </div>
          <div className="w-10 h-10 rounded-xl text-slate-500 hover:text-slate-300 flex items-center justify-center cursor-pointer hover:bg-slate-800/50 transition-colors">
            <Database className="w-5 h-5" />
          </div>
          <div className="w-10 h-10 rounded-xl text-slate-500 hover:text-slate-300 flex items-center justify-center cursor-pointer hover:bg-slate-800/50 transition-colors">
            <Server className="w-5 h-5" />
          </div>
          <div className="mt-auto w-10 h-10 rounded-xl text-slate-500 hover:text-slate-300 flex items-center justify-center cursor-pointer hover:bg-slate-800/50 transition-colors">
            <Settings className="w-5 h-5" />
          </div>
        </div>

        <PanelGroup direction="horizontal" className="flex-1">
          {/* Sidebar / Conversational Area */}
          <Panel defaultSize={25} minSize={20} className="bg-slate-900/50 flex flex-col">
            <div className="p-4 border-b border-slate-800/50 flex items-center gap-2">
              <span className="text-xs font-bold text-slate-400 uppercase tracking-widest">Orchestrator Chat</span>
            </div>
            
            <div className="flex-1 overflow-y-auto p-4 space-y-6">
              <div className="bg-slate-800/50 p-4 rounded-xl border border-slate-700/50 shadow-inner text-sm text-slate-300">
                Design an ML Pipeline where the Architect agent designs a simple CNN and the ML Agent implements and trains it...
              </div>
              
              <div className="flex items-start gap-3">
                <div className="w-6 h-6 mt-1 rounded-full bg-emerald-500/20 flex items-center justify-center border border-emerald-500/50 flex-shrink-0 shadow-[0_0_10px_rgba(16,185,129,0.2)]">
                  <Code2 className="w-3 h-3 text-emerald-400" />
                </div>
                <div className="space-y-3 flex-1">
                  <p className="text-sm text-slate-300 leading-relaxed">I have formulated a DAG workflow with 3 agents. <span className="text-emerald-400 font-medium">Architect</span>, <span className="text-blue-400 font-medium">Hermes Coder</span>, and <span className="text-purple-400 font-medium">ML Engineer</span> have been deployed.</p>
                  <button className="px-4 py-2 bg-emerald-500/10 hover:bg-emerald-500/20 border border-emerald-500/30 rounded-lg text-xs font-mono text-emerald-400 flex items-center gap-2 transition-colors">
                    <Play className="w-3 h-3" />
                    EXECUTE GRAPH
                  </button>
                </div>
              </div>
            </div>

            <div className="p-4 bg-slate-900/80 border-t border-slate-800/50">
              <div className="relative flex items-center">
                <input 
                  type="text" 
                  value={prompt}
                  onChange={(e) => setPrompt(e.target.value)}
                  placeholder="Ask Reticle..."
                  className="w-full bg-slate-950 border border-slate-700 text-slate-200 text-sm rounded-xl py-2.5 pl-4 pr-10 focus:outline-none focus:ring-1 focus:ring-emerald-500/50 transition-all placeholder-slate-600"
                />
                <button className="absolute right-1.5 p-1.5 rounded-lg hover:bg-emerald-500/20 text-emerald-500 transition-colors">
                  <Send className="w-4 h-4" />
                </button>
              </div>
            </div>
          </Panel>

          <PanelResizeHandle className="w-1 bg-slate-800/50 hover:bg-emerald-500/30 transition-colors cursor-col-resize" />

          {/* Main Editor & Graph Area */}
          <Panel defaultSize={75} className="flex flex-col bg-[#0b0f19]">
            <PanelGroup direction="vertical">
              <Panel defaultSize={70} className="relative">
                <div className="absolute top-4 left-4 z-10 flex gap-2">
                  <button className="px-3 py-1.5 rounded-md bg-slate-800 text-xs text-slate-300 border border-slate-700 hover:bg-slate-700 transition-colors shadow-lg">Graph View</button>
                  <button className="px-3 py-1.5 rounded-md bg-slate-900 text-xs text-slate-500 border border-slate-800 hover:bg-slate-800 transition-colors shadow-lg">src/train.py</button>
                </div>
                <GraphView />
              </Panel>
              
              <PanelResizeHandle className="h-1 bg-slate-800 hover:bg-emerald-500/30 transition-colors cursor-row-resize" />
              
              {/* Terminal / Output Panel */}
              <Panel defaultSize={30} className="bg-[#0f172a] border-t border-slate-800 flex flex-col">
                <div className="flex px-4 py-2 gap-4 border-b border-slate-800 bg-[#0b0f19]">
                  <span className="text-xs font-medium text-slate-300 border-b-2 border-emerald-500 pb-1 -mb-[9px]">TERMINAL</span>
                  <span className="text-xs font-medium text-slate-500 hover:text-slate-300 cursor-pointer">LOGS</span>
                  <span className="text-xs font-medium text-slate-500 hover:text-slate-300 cursor-pointer">PROBLEMS</span>
                </div>
                <div className="flex-1 p-4 font-mono text-xs text-slate-400 overflow-y-auto">
                  <div className="flex gap-2 mb-1"><span className="text-emerald-500">reticle&gt;</span><span>Starting workflow execution...</span></div>
                  <div className="flex gap-2 mb-1"><span className="text-blue-500">[Hermes]</span><span>Compiling src/train.py</span></div>
                  <div className="flex gap-2 mb-1"><span className="text-purple-500">[ML Eng]</span><span>Initializing PyTorch environment...</span></div>
                  <div className="flex gap-2"><span className="text-slate-500">2026-09-02 19:35:12</span><span className="text-yellow-400">WARN</span><span>GPU passthrough not detected. Training will fall back to CPU.</span></div>
                </div>
              </Panel>
            </PanelGroup>
          </Panel>
        </PanelGroup>
      </div>
    </div>
  );
}

export default App;
