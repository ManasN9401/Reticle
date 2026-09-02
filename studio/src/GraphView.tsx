import { useCallback } from 'react';
import ReactFlow, {
  MiniMap,
  Controls,
  Background,
  useNodesState,
  useEdgesState,
  addEdge,
} from 'reactflow';
import type { Connection, Edge } from 'reactflow';
import 'reactflow/dist/style.css';

const initialNodes = [
  { 
    id: 'architect', 
    position: { x: 250, y: 50 }, 
    data: { label: 'Architect Agent' },
    style: { background: '#1e1e2f', color: '#fff', border: '1px solid #4ade80', borderRadius: '8px' }
  },
  { 
    id: 'coder', 
    position: { x: 100, y: 200 }, 
    data: { label: 'Hermes Coder' },
    style: { background: '#1e1e2f', color: '#fff', border: '1px solid #60a5fa', borderRadius: '8px' }
  },
  { 
    id: 'ml', 
    position: { x: 400, y: 200 }, 
    data: { label: 'ML Engineer' },
    style: { background: '#1e1e2f', color: '#fff', border: '1px solid #c084fc', borderRadius: '8px' }
  },
];

const initialEdges = [
  { id: 'e1-2', source: 'architect', target: 'coder', animated: true, style: { stroke: '#4ade80' } },
  { id: 'e1-3', source: 'architect', target: 'ml', animated: true, style: { stroke: '#4ade80' } },
];

export default function GraphView() {
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  const onConnect = useCallback(
    (params: Connection | Edge) => setEdges((eds) => addEdge(params, eds)),
    [setEdges],
  );

  return (
    <div className="w-full h-full bg-slate-900 rounded-xl overflow-hidden border border-slate-800 shadow-2xl">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        fitView
        proOptions={{ hideAttribution: true }}
      >
        <Controls className="bg-slate-800 fill-slate-300 border-slate-700" />
        <MiniMap 
          nodeColor={(n) => {
            if (n.id === 'architect') return '#4ade80';
            if (n.id === 'coder') return '#60a5fa';
            return '#c084fc';
          }}
          maskColor="rgba(15, 23, 42, 0.7)"
          style={{ backgroundColor: '#1e293b' }}
        />
        <Background color="#334155" gap={16} />
      </ReactFlow>
    </div>
  );
}
