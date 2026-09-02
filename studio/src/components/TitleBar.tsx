import { Minus, Square, X, Target, Settings, Package, Database, Activity } from 'lucide-react';

export default function TitleBar() {
  const handleMinimize = () => (window as any).electronAPI?.minimize();
  const handleMaximize = () => (window as any).electronAPI?.maximize();
  const handleClose = () => (window as any).electronAPI?.close();

  return (
    <div 
      className="h-10 w-full bg-slate-950/80 backdrop-blur-xl border-b border-slate-800 flex items-center justify-between select-none z-50 flex-shrink-0"
      style={{ WebkitAppRegion: 'drag' } as any}
    >
      <div className="flex items-center px-4 gap-2 h-full">
        <Target className="w-4 h-4 text-emerald-400" />
        <span className="text-xs font-semibold text-slate-300 tracking-wider uppercase ml-1">Reticle</span>
      </div>

      <div className="flex items-center h-full" style={{ WebkitAppRegion: 'no-drag' } as any}>
        <div className="flex items-center justify-center w-12 h-full hover:bg-slate-800 text-slate-400 hover:text-white transition-colors cursor-pointer" onClick={handleMinimize}>
          <Minus className="w-3.5 h-3.5" />
        </div>
        <div className="flex items-center justify-center w-12 h-full hover:bg-slate-800 text-slate-400 hover:text-white transition-colors cursor-pointer" onClick={handleMaximize}>
          <Square className="w-3 h-3" />
        </div>
        <div className="flex items-center justify-center w-12 h-full hover:bg-red-500 hover:text-white text-slate-400 transition-colors cursor-pointer" onClick={handleClose}>
          <X className="w-4 h-4" />
        </div>
      </div>
    </div>
  );
}
