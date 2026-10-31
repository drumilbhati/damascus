import React, { useMemo, useState, useCallback, useEffect } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  Panel,
  useNodesState,
  useEdgesState,
  MarkerType,
  Position,
  Handle,
  ReactFlowProvider,
  useReactFlow,
} from '@xyflow/react';
import type { Node, Edge, NodeProps } from '@xyflow/react';
import dagre from '@dagrejs/dagre';
import {
  Server,
  Database,
  ShieldAlert,
  Search,
  RotateCcw,
  ArrowRight,
  ArrowLeft,
  X,
  Layers,
  Activity,
  Play,
  Maximize2,
} from 'lucide-react';
import { Link } from 'react-router-dom';
import type { DependencyGraph as GraphData, ServiceScore } from '../types';

import '@xyflow/react/dist/style.css';

// ---------------------------------------------------------------------------
// Helpers & Score Utilities
// ---------------------------------------------------------------------------

export interface ServiceNodeData extends Record<string, unknown> {
  name: string;
  score: number;
  reasons: string[];
  isSPOF: boolean;
  inDegree: number;
  outDegree: number;
  callCountIn: number;
  callCountOut: number;
  metadata?: Record<string, string>;
  direction: 'LR' | 'TB';
  isSelected?: boolean;
  isDimmed?: boolean;
}

export type ServiceNodeType = Node<ServiceNodeData, 'service'>;

interface CriticalityTier {
  label: string;
  badgeBg: string;
  badgeText: string;
  borderColor: string;
  barColor: string;
  dotColor: string;
}

const getCriticalityTier = (score: number): CriticalityTier => {
  if (score >= 0.7) {
    return {
      label: 'CRITICAL',
      badgeBg: 'rgba(248, 81, 73, 0.15)',
      badgeText: '#f85149',
      borderColor: 'rgba(248, 81, 73, 0.4)',
      barColor: '#f85149',
      dotColor: '#f85149',
    };
  }
  if (score >= 0.45) {
    return {
      label: 'HIGH',
      badgeBg: 'rgba(240, 136, 62, 0.15)',
      badgeText: '#f0883e',
      borderColor: 'rgba(240, 136, 62, 0.4)',
      barColor: '#f0883e',
      dotColor: '#f0883e',
    };
  }
  if (score >= 0.2) {
    return {
      label: 'MEDIUM',
      badgeBg: 'rgba(210, 153, 34, 0.15)',
      badgeText: '#d29922',
      borderColor: 'rgba(210, 153, 34, 0.4)',
      barColor: '#d29922',
      dotColor: '#d29922',
    };
  }
  return {
    label: 'LOW',
    badgeBg: 'rgba(63, 185, 80, 0.15)',
    badgeText: '#3fb950',
    borderColor: 'rgba(63, 185, 80, 0.35)',
    barColor: '#3fb950',
    dotColor: '#3fb950',
  };
};

const isStorageOrDatabase = (name: string, metadata?: Record<string, string>): boolean => {
  if (
    metadata?.type &&
    ['database', 'storage', 'cache', 'db'].includes(metadata.type.toLowerCase())
  ) {
    return true;
  }
  const lower = name.toLowerCase();
  // Match 'db' as a standalone token or bounded by delimiters (not an arbitrary substring like in 'feedback')
  const dbTokenRegex = /(^|[._:-])db([._:-]|$)/i;
  return (
    dbTokenRegex.test(lower) ||
    lower.includes('database') ||
    lower.includes('redis') ||
    lower.includes('postgres') ||
    lower.includes('mysql') ||
    lower.includes('mongo') ||
    lower.includes('storage') ||
    lower.includes('cache')
  );
};

// ---------------------------------------------------------------------------
// Custom Node Component
// ---------------------------------------------------------------------------

const ServiceNodeComponent: React.FC<NodeProps<ServiceNodeType>> = ({
  data,
  selected,
}) => {
  const {
    name,
    score,
    isSPOF,
    inDegree,
    outDegree,
    direction = 'LR',
    isDimmed,
    metadata,
  } = data;

  const tier = getCriticalityTier(score);
  const isDb = isStorageOrDatabase(name, metadata);
  const scorePercent = (score * 100).toFixed(0);

  const isHorizontal = direction === 'LR';

  return (
    <div
      className={`relative w-[230px] rounded-lg transition-all duration-200 select-none shadow-lg ${
        isDimmed ? 'opacity-30 filter grayscale' : 'opacity-100'
      }`}
      style={{
        backgroundColor: '#161b22',
        border: selected
          ? '2px solid #58a6ff'
          : `1px solid ${tier.borderColor || '#30363d'}`,
        boxShadow: selected
          ? '0 0 16px rgba(88, 166, 255, 0.35)'
          : isSPOF
          ? '0 0 10px rgba(248, 81, 73, 0.2)'
          : '0 4px 12px rgba(0, 0, 0, 0.3)',
      }}
    >
      {/* React Flow Connection Handles */}
      <Handle
        type="target"
        position={isHorizontal ? Position.Left : Position.Top}
        className="!w-2.5 !h-2.5 !bg-[#58a6ff] !border-2 !border-[#0d1117] transition-transform hover:scale-125"
      />
      <Handle
        type="source"
        position={isHorizontal ? Position.Right : Position.Bottom}
        className="!w-2.5 !h-2.5 !bg-[#bc8cff] !border-2 !border-[#0d1117] transition-transform hover:scale-125"
      />

      <div className="p-3">
        {/* Node Top Row: Icon + Service Name + Criticality Badge */}
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <div
              className="flex h-7 w-7 items-center justify-center rounded shrink-0"
              style={{
                backgroundColor: isDb ? '#21262d' : '#1c2128',
                border: '1px solid #30363d',
                color: isDb ? '#58a6ff' : '#e6edf3',
              }}
            >
              {isDb ? (
                <Database className="h-3.5 w-3.5" />
              ) : (
                <Server className="h-3.5 w-3.5" />
              )}
            </div>
            <div className="min-w-0">
              <div
                className="font-mono text-xs font-semibold truncate leading-tight"
                style={{ color: '#e6edf3' }}
                title={name}
              >
                {name}
              </div>
              <div className="text-[10px] font-mono leading-none mt-1" style={{ color: '#8b949e' }}>
                {inDegree} in · {outDegree} out
              </div>
            </div>
          </div>

          {/* Criticality Score Badge */}
          <div className="flex flex-col items-end shrink-0">
            <span
              className="rounded px-1.5 py-0.5 font-mono text-[10px] font-bold tracking-tight"
              style={{
                backgroundColor: tier.badgeBg,
                color: tier.badgeText,
                border: `1px solid ${tier.borderColor}`,
              }}
              title={`Blast-radius criticality score: ${(score * 100).toFixed(1)}%`}
            >
              {scorePercent}%
            </span>
          </div>
        </div>

        {/* SPOF & Criticality Alert Flags */}
        {isSPOF && (
          <div className="mt-2 flex items-center gap-1 rounded px-1.5 py-0.5 text-[9px] font-semibold tracking-wider uppercase font-mono"
            style={{
              backgroundColor: 'rgba(248, 81, 73, 0.15)',
              color: '#f85149',
              border: '1px solid rgba(248, 81, 73, 0.3)',
            }}
          >
            <ShieldAlert className="h-3 w-3 shrink-0" />
            <span>Single Point of Failure</span>
          </div>
        )}

        {/* Criticality Score Bar */}
        <div className="mt-2.5">
          <div className="h-1 w-full rounded-full overflow-hidden" style={{ backgroundColor: '#21262d' }}>
            <div
              className="h-full rounded-full transition-all duration-300"
              style={{
                width: `${Math.max(6, Math.min(100, score * 100))}%`,
                backgroundColor: tier.barColor,
              }}
            />
          </div>
        </div>
      </div>
    </div>
  );
};

// ---------------------------------------------------------------------------
// Dagre Layout Calculation
// ---------------------------------------------------------------------------

const NODE_WIDTH = 230;
const NODE_HEIGHT = 90;

const applyDagreLayout = (
  nodes: Node<ServiceNodeData>[],
  edges: Edge[],
  direction: 'LR' | 'TB' = 'LR'
): { nodes: Node<ServiceNodeData>[]; edges: Edge[] } => {
  const dagreGraph = new dagre.graphlib.Graph();
  dagreGraph.setDefaultEdgeLabel(() => ({}));

  const isHorizontal = direction === 'LR';
  dagreGraph.setGraph({
    rankdir: direction,
    nodesep: isHorizontal ? 50 : 60,
    ranksep: isHorizontal ? 90 : 80,
    marginx: 40,
    marginy: 40,
  });

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  });

  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target);
  });

  dagre.layout(dagreGraph);

  const layoutedNodes = nodes.map((node) => {
    const nodeWithPosition = dagreGraph.node(node.id);
    const x = nodeWithPosition
      ? nodeWithPosition.x - NODE_WIDTH / 2
      : Math.random() * 400;
    const y = nodeWithPosition
      ? nodeWithPosition.y - NODE_HEIGHT / 2
      : Math.random() * 400;

    return {
      ...node,
      targetPosition: isHorizontal ? Position.Left : Position.Top,
      sourcePosition: isHorizontal ? Position.Right : Position.Bottom,
      data: {
        ...node.data,
        direction,
      },
      position: { x, y },
    };
  });

  return { nodes: layoutedNodes, edges };
};

// ---------------------------------------------------------------------------
// Dependency Graph Inner Flow Component
// ---------------------------------------------------------------------------

export interface DependencyGraphProps {
  graph?: GraphData;
  scores?: ServiceScore[];
  selectedService?: string | null;
  onSelectService?: (serviceName: string | null) => void;
  className?: string;
  height?: number | string;
  showInspector?: boolean;
}

const DEFAULT_SCORES: ServiceScore[] = [];

const nodeTypes = {
  service: ServiceNodeComponent,
};

const DependencyGraphInternal: React.FC<DependencyGraphProps> = ({
  graph,
  scores = DEFAULT_SCORES,
  selectedService: controlledSelectedService,
  onSelectService,
  className = '',
  height = 580,
  showInspector = true,
}) => {
  const { fitView, setCenter } = useReactFlow();

  const [direction, setDirection] = useState<'LR' | 'TB'>('LR');
  const [searchQuery, setSearchQuery] = useState('');
  const [criticalityFilter, setCriticalityFilter] = useState<'all' | 'high' | 'spof'>('all');
  const [animatedTraffic, setAnimatedTraffic] = useState(true);
  const [internalSelectedService, setInternalSelectedService] = useState<string | null>(null);

  const activeSelected = controlledSelectedService !== undefined
    ? controlledSelectedService
    : internalSelectedService;

  const handleSelectService = useCallback(
    (name: string | null) => {
      setInternalSelectedService(name);
      onSelectService?.(name);
    },
    [onSelectService]
  );

  // Map scores by service name for O(1) lookup
  const scoresMap = useMemo(() => {
    const map = new Map<string, ServiceScore>();
    scores.forEach((s) => map.set(s.service_name, s));
    return map;
  }, [scores]);

  // Aggregate raw nodes and edges
  const { rawNodes, rawEdges } = useMemo(() => {
    const rawEdges = graph?.edges || [];
    const nodeNames = new Set<string>();

    if (graph?.nodes) {
      Object.keys(graph.nodes).forEach((k) => nodeNames.add(k));
    }
    rawEdges.forEach((e) => {
      if (e.from) nodeNames.add(e.from);
      if (e.to) nodeNames.add(e.to);
    });

    const inCallers = new Map<string, Set<string>>();
    const outCallees = new Map<string, Set<string>>();
    const callsIn = new Map<string, number>();
    const callsOut = new Map<string, number>();

    rawEdges.forEach((e) => {
      if (!outCallees.has(e.from)) outCallees.set(e.from, new Set());
      if (!inCallers.has(e.to)) inCallers.set(e.to, new Set());
      outCallees.get(e.from)!.add(e.to);
      inCallers.get(e.to)!.add(e.from);

      callsOut.set(e.from, (callsOut.get(e.from) || 0) + e.call_count);
      callsIn.set(e.to, (callsIn.get(e.to) || 0) + e.call_count);
    });

    const rawNodes: Node<ServiceNodeData>[] = Array.from(nodeNames).map((name) => {
      const scoreObj = scoresMap.get(name);
      const scoreVal = scoreObj ? scoreObj.score : 0;
      const reasons = scoreObj?.reasons || [];
      const isSPOF = reasons.some(
        (r) =>
          r.toLowerCase().includes('spof') ||
          r.toLowerCase().includes('single point of failure')
      );

      return {
        id: name,
        type: 'service' as const,
        position: { x: 0, y: 0 },
        data: {
          name,
          score: scoreVal,
          reasons,
          isSPOF,
          inDegree: inCallers.get(name)?.size || 0,
          outDegree: outCallees.get(name)?.size || 0,
          callCountIn: callsIn.get(name) || 0,
          callCountOut: callsOut.get(name) || 0,
          metadata: graph?.nodes?.[name]?.metadata,
          direction,
        },
      };
    });

    return { rawNodes, rawEdges };
  }, [graph, scoresMap, direction]);

  const [layoutResetToken, setLayoutResetToken] = useState(0);

  // Compute Dagre layout positions only when graph topology, layout direction, or reset token changes
  const dagrePositions = useMemo(() => {
    if (rawNodes.length === 0) return new Map<string, { x: number; y: number }>();
    // layoutResetToken triggers re-layout calculation when incremented
    if (layoutResetToken < 0) return new Map();
    const dummyEdges: Edge[] = rawEdges.map((e, idx) => ({
      id: `dagre-e-${idx}`,
      source: e.from,
      target: e.to,
    }));
    const { nodes: layouted } = applyDagreLayout(rawNodes, dummyEdges, direction);
    const map = new Map<string, { x: number; y: number }>();
    layouted.forEach((n) => map.set(n.id, n.position));
    return map;
  }, [rawNodes, rawEdges, direction, layoutResetToken]);

  const [nodes, setNodes, onNodesChange] = useNodesState<ServiceNodeType>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  // Update nodes while preserving user-dragged positions across search, filter, and selection changes
  useEffect(() => {
    if (rawNodes.length === 0) {
      setNodes([]);
      return;
    }

    const connectedNodeIds = new Set<string>();
    if (activeSelected) {
      connectedNodeIds.add(activeSelected);
      rawEdges.forEach((e) => {
        if (e.from === activeSelected) connectedNodeIds.add(e.to);
        if (e.to === activeSelected) connectedNodeIds.add(e.from);
      });
    }

    const isHorizontal = direction === 'LR';

    setNodes((prevNodes) => {
      // Retain existing node positions (which include positions modified by drag)
      const prevPosMap = new Map(prevNodes.map((pn) => [pn.id, pn.position]));

      return rawNodes.map((n) => {
        const isSelected = n.id === activeSelected;
        const matchesSearch = searchQuery
          ? n.id.toLowerCase().includes(searchQuery.toLowerCase())
          : true;
        const matchesFilter =
          criticalityFilter === 'all'
            ? true
            : criticalityFilter === 'high'
            ? n.data.score >= 0.45
            : n.data.isSPOF;

        const isDimmed =
          (activeSelected ? !connectedNodeIds.has(n.id) : false) ||
          !matchesSearch ||
          !matchesFilter;

        const pos = prevPosMap.get(n.id) ?? dagrePositions.get(n.id) ?? { x: 0, y: 0 };

        return {
          ...n,
          type: 'service' as const,
          selected: isSelected,
          targetPosition: isHorizontal ? Position.Left : Position.Top,
          sourcePosition: isHorizontal ? Position.Right : Position.Bottom,
          position: pos,
          data: {
            ...n.data,
            isSelected,
            isDimmed,
            direction,
          },
        };
      });
    });
  }, [
    rawNodes,
    rawEdges,
    dagrePositions,
    activeSelected,
    searchQuery,
    criticalityFilter,
    direction,
    setNodes,
  ]);

  // Update edges with directional arrows, call volume scaling, and dimming for filtered/dimmed nodes
  useEffect(() => {
    if (rawNodes.length === 0) {
      setEdges([]);
      return;
    }

    const connectedNodeIds = new Set<string>();
    if (activeSelected) {
      connectedNodeIds.add(activeSelected);
      rawEdges.forEach((e) => {
        if (e.from === activeSelected) connectedNodeIds.add(e.to);
        if (e.to === activeSelected) connectedNodeIds.add(e.from);
      });
    }

    // Determine dimmed node IDs from active filter/search/selection
    const dimmedNodeIds = new Set<string>();
    rawNodes.forEach((n) => {
      const matchesSearch = searchQuery
        ? n.id.toLowerCase().includes(searchQuery.toLowerCase())
        : true;
      const matchesFilter =
        criticalityFilter === 'all'
          ? true
          : criticalityFilter === 'high'
          ? n.data.score >= 0.45
          : n.data.isSPOF;

      const isDimmed =
        (activeSelected ? !connectedNodeIds.has(n.id) : false) ||
        !matchesSearch ||
        !matchesFilter;

      if (isDimmed) {
        dimmedNodeIds.add(n.id);
      }
    });

    const processedEdges: Edge[] = rawEdges.map((e, idx) => {
      const isConnected =
        activeSelected && (e.from === activeSelected || e.to === activeSelected);
      const touchesDimmedNode = dimmedNodeIds.has(e.from) || dimmedNodeIds.has(e.to);
      const isDimmed = Boolean((activeSelected && !isConnected) || touchesDimmedNode);

      const strokeColor = isConnected
        ? '#58a6ff'
        : e.frequency > 50
        ? '#bc8cff'
        : '#388bfd';

      const strokeWidth = Math.min(3.5, Math.max(1.5, Math.log10(e.call_count + 1) * 0.9));

      return {
        id: `e-${e.from}-${e.to}-${idx}`,
        source: e.from,
        target: e.to,
        type: 'smoothstep',
        animated: animatedTraffic && !isDimmed,
        style: {
          stroke: isDimmed ? '#21262d' : strokeColor,
          strokeWidth,
          opacity: isDimmed ? 0.2 : 0.85,
        },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 16,
          height: 16,
          color: isDimmed ? '#21262d' : strokeColor,
        },
        label: `${e.call_count.toLocaleString()} calls`,
        labelStyle: {
          fill: isDimmed ? '#484f58' : '#8b949e',
          fontSize: 10,
          fontFamily: 'monospace',
          fontWeight: 600,
        },
        labelBgStyle: {
          fill: '#161b22',
          fillOpacity: 0.92,
          stroke: isDimmed ? '#21262d' : '#30363d',
          strokeWidth: 1,
          rx: 4,
          ry: 4,
        },
        labelBgPadding: [4, 6] as [number, number],
      };
    });

    setEdges(processedEdges);
  }, [
    rawNodes,
    rawEdges,
    activeSelected,
    searchQuery,
    criticalityFilter,
    animatedTraffic,
    setEdges,
  ]);

  // Center on selected node if selected
  useEffect(() => {
    if (activeSelected) {
      const targetNode = nodes.find((n) => n.id === activeSelected);
      if (targetNode) {
        setCenter(targetNode.position.x + NODE_WIDTH / 2, targetNode.position.y + NODE_HEIGHT / 2, {
          duration: 600,
          zoom: 1.1,
        });
      }
    }
  }, [activeSelected, nodes, setCenter]);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      handleSelectService(node.id === activeSelected ? null : node.id);
    },
    [activeSelected, handleSelectService]
  );

  const onPaneClick = useCallback(() => {
    handleSelectService(null);
  }, [handleSelectService]);

  const handleResetView = useCallback(() => {
    handleSelectService(null);
    setSearchQuery('');
    setCriticalityFilter('all');
    setLayoutResetToken((t) => t + 1);
    setNodes((prevNodes) =>
      prevNodes.map((n) => ({
        ...n,
        position: dagrePositions.get(n.id) ?? n.position,
      }))
    );
    setTimeout(() => {
      fitView({ padding: 0.2, duration: 500 });
    }, 50);
  }, [fitView, handleSelectService, dagrePositions, setNodes]);

  // Selected Service Details for Inspector Drawer
  const selectedNodeDetails = useMemo(() => {
    if (!activeSelected) return null;
    const node = rawNodes.find((n) => n.id === activeSelected);
    if (!node) return null;

    const incoming = rawEdges.filter((e) => e.to === activeSelected);
    const outgoing = rawEdges.filter((e) => e.from === activeSelected);

    return {
      node,
      incoming,
      outgoing,
      tier: getCriticalityTier(node.data.score),
    };
  }, [activeSelected, rawNodes, rawEdges]);

  return (
    <div
      className={`relative w-full rounded-lg overflow-hidden flex flex-col ${className}`}
      style={{
        height,
        backgroundColor: '#0d1117',
        border: '1px solid #30363d',
      }}
    >
      {/* Top Action / Filter Toolbar */}
      <div
        className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5 z-10"
        style={{
          backgroundColor: '#161b22',
          borderBottom: '1px solid #30363d',
        }}
      >
        <div className="flex items-center gap-2">
          {/* Service Search Box */}
          <div className="relative">
            <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-[#6e7681]" />
            <input
              type="text"
              placeholder="Search service..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="rounded pl-8 pr-2.5 py-1 text-xs font-mono outline-none w-44"
              style={{
                backgroundColor: '#21262d',
                border: '1px solid #30363d',
                color: '#e6edf3',
              }}
            />
            {searchQuery && (
              <button
                type="button"
                onClick={() => setSearchQuery('')}
                aria-label="Clear search"
                className="absolute right-2 top-2 text-[#6e7681] hover:text-[#e6edf3]"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          {/* Criticality Filter */}
          <div className="flex items-center rounded overflow-hidden" style={{ border: '1px solid #30363d' }}>
            <button
              onClick={() => setCriticalityFilter('all')}
              className="px-2.5 py-1 text-xs font-semibold"
              style={{
                backgroundColor: criticalityFilter === 'all' ? '#30363d' : '#21262d',
                color: criticalityFilter === 'all' ? '#e6edf3' : '#8b949e',
              }}
            >
              All ({rawNodes.length})
            </button>
            <button
              onClick={() => setCriticalityFilter('high')}
              className="px-2.5 py-1 text-xs font-semibold"
              style={{
                backgroundColor: criticalityFilter === 'high' ? '#30363d' : '#21262d',
                color: criticalityFilter === 'high' ? '#f0883e' : '#8b949e',
              }}
            >
              High Criticality (≥45%)
            </button>
            <button
              onClick={() => setCriticalityFilter('spof')}
              className="px-2.5 py-1 text-xs font-semibold flex items-center gap-1"
              style={{
                backgroundColor: criticalityFilter === 'spof' ? '#30363d' : '#21262d',
                color: criticalityFilter === 'spof' ? '#f85149' : '#8b949e',
              }}
            >
              <ShieldAlert className="h-3 w-3" />
              SPOFs
            </button>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {/* Layout Direction Toggle */}
          <div className="flex items-center rounded overflow-hidden" style={{ border: '1px solid #30363d' }}>
            <button
              onClick={() => setDirection('LR')}
              className="px-2.5 py-1 text-xs font-mono"
              style={{
                backgroundColor: direction === 'LR' ? '#30363d' : '#21262d',
                color: direction === 'LR' ? '#e6edf3' : '#8b949e',
              }}
              title="Horizontal Hierarchy"
            >
              Left → Right
            </button>
            <button
              onClick={() => setDirection('TB')}
              className="px-2.5 py-1 text-xs font-mono"
              style={{
                backgroundColor: direction === 'TB' ? '#30363d' : '#21262d',
                color: direction === 'TB' ? '#e6edf3' : '#8b949e',
              }}
              title="Top → Bottom Hierarchy"
            >
              Top ↓ Bottom
            </button>
          </div>

          {/* Animated Traffic Toggle */}
          <button
            onClick={() => setAnimatedTraffic((prev) => !prev)}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded text-xs font-semibold"
            style={{
              backgroundColor: animatedTraffic ? 'rgba(88, 166, 255, 0.12)' : '#21262d',
              border: `1px solid ${animatedTraffic ? 'rgba(88, 166, 255, 0.4)' : '#30363d'}`,
              color: animatedTraffic ? '#58a6ff' : '#8b949e',
            }}
            title="Toggle animated traffic flow"
          >
            <Activity className="h-3 w-3" />
            <span>Traffic Flow</span>
          </button>

          {/* Reset / Fit View */}
          <button
            onClick={handleResetView}
            className="flex h-7 w-7 items-center justify-center rounded"
            style={{
              backgroundColor: '#21262d',
              border: '1px solid #30363d',
              color: '#8b949e',
            }}
            title="Reset layout & fit view"
          >
            <RotateCcw className="h-3.5 w-3.5" />
          </button>

          <button
            onClick={() => fitView({ padding: 0.2, duration: 400 })}
            className="flex h-7 w-7 items-center justify-center rounded"
            style={{
              backgroundColor: '#21262d',
              border: '1px solid #30363d',
              color: '#8b949e',
            }}
            title="Fit to screen"
          >
            <Maximize2 className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      {/* Main Flow Canvas */}
      <div className="relative flex-1 w-full h-full">
        {rawNodes.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 text-xs" style={{ color: '#6e7681' }}>
            <Layers className="h-8 w-8 text-[#30363d]" />
            <span>No service nodes found in topology</span>
            <span className="text-[11px] text-[#484f58]">Run mesh traffic or Jaeger traces to reconstruct the dependency graph</span>
          </div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={onNodeClick}
            onPaneClick={onPaneClick}
            nodeTypes={nodeTypes}
            fitView
            minZoom={0.2}
            maxZoom={2}
            defaultEdgeOptions={{
              type: 'smoothstep',
            }}
          >
            <Background color="#30363d" gap={20} size={1} />
            <Controls
              style={{
                backgroundColor: '#161b22',
                border: '1px solid #30363d',
                borderRadius: '6px',
                fill: '#8b949e',
              }}
              showInteractive={false}
            />
            <MiniMap
              style={{
                backgroundColor: '#161b22',
                border: '1px solid #30363d',
                borderRadius: '6px',
              }}
              nodeColor={(n) => {
                const s = (n.data as ServiceNodeData)?.score ?? 0;
                const tier = getCriticalityTier(s);
                return tier.dotColor;
              }}
              maskColor="rgba(13, 17, 23, 0.75)"
              zoomable
              pannable
            />

            {/* Bottom Legend Panel */}
            <Panel position="bottom-left">
              <div
                className="flex items-center gap-3 px-3 py-1.5 rounded text-[10px] font-mono shadow-md backdrop-blur-sm"
                style={{
                  backgroundColor: 'rgba(22, 27, 34, 0.88)',
                  border: '1px solid #30363d',
                  color: '#8b949e',
                }}
              >
                <span className="font-semibold text-[#e6edf3]">Criticality:</span>
                <span className="flex items-center gap-1">
                  <span className="h-2 w-2 rounded-full bg-[#f85149]" />
                  <span>≥70%</span>
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-2 w-2 rounded-full bg-[#f0883e]" />
                  <span>45-69%</span>
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-2 w-2 rounded-full bg-[#d29922]" />
                  <span>20-44%</span>
                </span>
                <span className="flex items-center gap-1">
                  <span className="h-2 w-2 rounded-full bg-[#3fb950]" />
                  <span>&lt;20%</span>
                </span>
                <span className="flex items-center gap-1 pl-2 border-l border-[#30363d]">
                  <ArrowRight className="h-3 w-3 text-[#58a6ff]" />
                  <span>Caller → Callee</span>
                </span>
              </div>
            </Panel>
          </ReactFlow>
        )}

        {/* Selected Service Inspector Drawer (Right Side) */}
        {showInspector && selectedNodeDetails && (
          <div
            className="absolute top-0 right-0 bottom-0 w-80 max-w-[calc(100%-1rem)] z-20 flex flex-col shadow-2xl backdrop-blur-md overflow-hidden"
            style={{
              backgroundColor: 'rgba(22, 27, 34, 0.95)',
              borderLeft: '1px solid #30363d',
            }}
          >
            {/* Header */}
            <div
              className="flex items-center justify-between px-4 py-3 shrink-0"
              style={{ borderBottom: '1px solid #30363d' }}
            >
              <div className="flex items-center gap-2 min-w-0">
                <div
                  className="flex h-6 w-6 items-center justify-center rounded shrink-0"
                  style={{
                    backgroundColor: '#21262d',
                    border: '1px solid #30363d',
                    color: '#58a6ff',
                  }}
                >
                  <Server className="h-3 w-3" />
                </div>
                <div className="min-w-0">
                  <span className="font-mono text-xs font-semibold block truncate" style={{ color: '#e6edf3' }}>
                    {selectedNodeDetails.node.data.name}
                  </span>
                  <span className="text-[10px] text-[#8b949e]">Service Node Details</span>
                </div>
              </div>
              <button
                type="button"
                onClick={() => handleSelectService(null)}
                aria-label="Close service inspector"
                className="text-[#8b949e] hover:text-[#e6edf3] p-1 rounded"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {/* Inspector Body */}
            <div className="p-4 space-y-4 overflow-y-auto flex-1 text-xs">
              {/* Criticality Score Summary */}
              <div
                className="rounded p-3"
                style={{
                  backgroundColor: '#161b22',
                  border: `1px solid ${selectedNodeDetails.tier.borderColor}`,
                }}
              >
                <div className="flex items-center justify-between">
                  <span className="text-[10px] font-semibold uppercase tracking-wider text-[#8b949e]">
                    Criticality Score
                  </span>
                  <span
                    className="rounded px-2 py-0.5 font-mono text-xs font-bold"
                    style={{
                      backgroundColor: selectedNodeDetails.tier.badgeBg,
                      color: selectedNodeDetails.tier.badgeText,
                      border: `1px solid ${selectedNodeDetails.tier.borderColor}`,
                    }}
                  >
                    {(selectedNodeDetails.node.data.score * 100).toFixed(0)}%
                  </span>
                </div>

                <div className="mt-2 h-1.5 w-full rounded-full overflow-hidden bg-[#21262d]">
                  <div
                    className="h-full rounded-full"
                    style={{
                      width: `${Math.max(6, selectedNodeDetails.node.data.score * 100)}%`,
                      backgroundColor: selectedNodeDetails.tier.barColor,
                    }}
                  />
                </div>

                {selectedNodeDetails.node.data.isSPOF && (
                  <div className="mt-2.5 flex items-center gap-1.5 text-[11px] font-semibold text-[#f85149]">
                    <ShieldAlert className="h-3.5 w-3.5 shrink-0" />
                    <span>Identified Single Point of Failure</span>
                  </div>
                )}
              </div>

              {/* Explanatory Reasons Breakdown */}
              {selectedNodeDetails.node.data.reasons.length > 0 && (
                <div>
                  <div className="text-[10px] font-semibold uppercase tracking-wider text-[#6e7681] mb-1.5">
                    Scoring Rationale
                  </div>
                  <div
                    className="rounded p-2.5 space-y-1.5"
                    style={{ backgroundColor: '#21262d', border: '1px solid #30363d' }}
                  >
                    {selectedNodeDetails.node.data.reasons.map((r, i) => (
                      <div key={i} className="flex items-start gap-1.5 text-[11px]" style={{ color: '#e6edf3' }}>
                        <span className="text-[#bc8cff] mt-0.5">•</span>
                        <span>{r}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Mesh Call Volume */}
              <div>
                <div className="text-[10px] font-semibold uppercase tracking-wider text-[#6e7681] mb-1.5">
                  Mesh Traffic Volume
                </div>
                <div className="grid grid-cols-2 gap-2">
                  <div className="rounded p-2" style={{ backgroundColor: '#21262d', border: '1px solid #30363d' }}>
                    <div className="text-[10px] text-[#8b949e]">Total Inbound</div>
                    <div className="font-mono text-sm font-bold text-[#e6edf3] mt-0.5">
                      {selectedNodeDetails.node.data.callCountIn.toLocaleString()}
                    </div>
                    <div className="text-[10px] text-[#6e7681] font-mono">
                      from {selectedNodeDetails.node.data.inDegree} services
                    </div>
                  </div>
                  <div className="rounded p-2" style={{ backgroundColor: '#21262d', border: '1px solid #30363d' }}>
                    <div className="text-[10px] text-[#8b949e]">Total Outbound</div>
                    <div className="font-mono text-sm font-bold text-[#e6edf3] mt-0.5">
                      {selectedNodeDetails.node.data.callCountOut.toLocaleString()}
                    </div>
                    <div className="text-[10px] text-[#6e7681] font-mono">
                      to {selectedNodeDetails.node.data.outDegree} services
                    </div>
                  </div>
                </div>
              </div>

              {/* Inbound Callers */}
              <div>
                <div className="text-[10px] font-semibold uppercase tracking-wider text-[#6e7681] mb-1.5 flex items-center justify-between">
                  <span className="flex items-center gap-1">
                    <ArrowLeft className="h-3 w-3 text-[#58a6ff]" />
                    Inbound Callers ({selectedNodeDetails.incoming.length})
                  </span>
                </div>
                <div
                  className="rounded overflow-hidden"
                  style={{ border: '1px solid #30363d', backgroundColor: '#21262d' }}
                >
                  {selectedNodeDetails.incoming.length === 0 ? (
                    <div className="p-3 text-[11px] text-[#6e7681] text-center">
                      Root service (no upstream callers observed)
                    </div>
                  ) : (
                    selectedNodeDetails.incoming.map((e, idx) => (
                      <div
                        key={idx}
                        role="button"
                        tabIndex={0}
                        aria-label={`Select caller service ${e.from}`}
                        className="px-3 py-2 flex items-center justify-between hover:bg-[#161b22] cursor-pointer focus:outline-none focus:bg-[#161b22]"
                        style={{
                          borderBottom:
                            idx < selectedNodeDetails.incoming.length - 1
                              ? '1px solid #30363d'
                              : 'none',
                        }}
                        onClick={() => handleSelectService(e.from)}
                        onKeyDown={(ev) => {
                          if (ev.key === 'Enter' || ev.key === ' ') {
                            ev.preventDefault();
                            handleSelectService(e.from);
                          }
                        }}
                      >
                        <span className="font-mono text-xs font-semibold text-[#e6edf3]">
                          {e.from}
                        </span>
                        <span className="font-mono text-[10px] text-[#8b949e]">
                          {e.call_count.toLocaleString()} reqs
                        </span>
                      </div>
                    ))
                  )}
                </div>
              </div>

              {/* Outbound Callees */}
              <div>
                <div className="text-[10px] font-semibold uppercase tracking-wider text-[#6e7681] mb-1.5 flex items-center justify-between">
                  <span className="flex items-center gap-1">
                    <ArrowRight className="h-3 w-3 text-[#bc8cff]" />
                    Outbound Callees ({selectedNodeDetails.outgoing.length})
                  </span>
                </div>
                <div
                  className="rounded overflow-hidden"
                  style={{ border: '1px solid #30363d', backgroundColor: '#21262d' }}
                >
                  {selectedNodeDetails.outgoing.length === 0 ? (
                    <div className="p-3 text-[11px] text-[#6e7681] text-center">
                      Leaf service (no downstream calls observed)
                    </div>
                  ) : (
                    selectedNodeDetails.outgoing.map((e, idx) => (
                      <div
                        key={idx}
                        role="button"
                        tabIndex={0}
                        aria-label={`Select callee service ${e.to}`}
                        className="px-3 py-2 flex items-center justify-between hover:bg-[#161b22] cursor-pointer focus:outline-none focus:bg-[#161b22]"
                        style={{
                          borderBottom:
                            idx < selectedNodeDetails.outgoing.length - 1
                              ? '1px solid #30363d'
                              : 'none',
                        }}
                        onClick={() => handleSelectService(e.to)}
                        onKeyDown={(ev) => {
                          if (ev.key === 'Enter' || ev.key === ' ') {
                            ev.preventDefault();
                            handleSelectService(e.to);
                          }
                        }}
                      >
                        <span className="font-mono text-xs font-semibold text-[#58a6ff]">
                          {e.to}
                        </span>
                        <span className="font-mono text-[10px] text-[#8b949e]">
                          {e.call_count.toLocaleString()} reqs
                        </span>
                      </div>
                    ))
                  )}
                </div>
              </div>

              {/* Action Button: Target this service with Chaos Experiment */}
              <div className="pt-2">
                <Link
                  to={`/experiments?target=${encodeURIComponent(selectedNodeDetails.node.data.name)}`}
                  className="flex items-center justify-center gap-1.5 w-full py-2 rounded text-xs font-semibold transition-colors"
                  style={{ backgroundColor: '#f0883e', color: '#0d1117' }}
                >
                  <Play className="h-3 w-3 fill-current" />
                  Target in Chaos Experiment
                </Link>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

// Wrap with ReactFlowProvider to provide React Flow context
export const DependencyGraph: React.FC<DependencyGraphProps> = (props) => {
  return (
    <ReactFlowProvider>
      <DependencyGraphInternal {...props} />
    </ReactFlowProvider>
  );
};

export default DependencyGraph;
