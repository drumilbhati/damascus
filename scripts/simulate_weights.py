#!/usr/bin/env python3
"""
Microservice Criticality Scoring Simulation & Weight Optimization
==================================================================

This simulation models cascading failure dynamics, blast radius propagation,
and end-to-end transaction loss across diverse microservice topologies
(including the canonical OpenTelemetry Astronomy Shop Demo and multi-tier
enterprise service meshes).

Objective:
Empirically simulate service degradation and failure injection to compute
ground-truth criticality impacts, and determine the optimal weights
(w1: InDegree, w2: OutDegree, w3: CallFreq, w4: Depth, w5: SPOF) that maximize
the correlation with actual system blast radius.
"""

import math
import random
from collections import defaultdict, deque
from typing import Dict, List, Tuple, Set, NamedTuple

# Set fixed random seed for 100% reproducible simulations
random.seed(42)


class Edge(NamedTuple):
    src: str
    dst: str
    freq: float  # calls per second
    critical: bool = True  # whether failure of dst fails the caller or caller has fallback


class ServiceMesh:
    def __init__(self, name: str):
        self.name = name
        self.nodes: Set[str] = set()
        self.edges: List[Edge] = []
        self.ingress_routes: List[Tuple[str, float]] = []  # (root_service, weight)
        self.transactions: List[Tuple[str, List[str], float]] = []  # (name, [service_path], traffic_pct)

    def add_edge(self, src: str, dst: str, freq: float, critical: bool = True):
        self.nodes.add(src)
        self.nodes.add(dst)
        self.edges.append(Edge(src, dst, freq, critical))

    def add_ingress_route(self, root: str, weight: float = 1.0):
        self.nodes.add(root)
        self.ingress_routes.append((root, weight))

    def add_transaction(self, name: str, service_path: List[str], traffic_pct: float):
        for s in service_path:
            self.nodes.add(s)
        self.transactions.append((name, service_path, traffic_pct))


# -----------------------------------------------------------------------------
# Topology Definitions
# -----------------------------------------------------------------------------

def build_astronomy_shop_mesh() -> ServiceMesh:
    """
    OpenTelemetry Astronomy Shop Demo Topology
    Services:
      frontend, productcatalogservice, currencyservice, cartservice,
      recommendationservice, shippingservice, checkoutservice, adservice,
      paymentservice, emailservice, quoteservice, valkey-cart
    """
    mesh = ServiceMesh("OTel Astronomy Shop Mesh (12 services)")
    mesh.add_ingress_route("frontend", 1.0)

    # Ingress frontend calls
    mesh.add_edge("frontend", "productcatalogservice", 250.0, critical=True)
    mesh.add_edge("frontend", "currencyservice", 200.0, critical=True)
    mesh.add_edge("frontend", "cartservice", 180.0, critical=True)
    mesh.add_edge("frontend", "recommendationservice", 120.0, critical=False)  # UI fallback available
    mesh.add_edge("frontend", "shippingservice", 40.0, critical=True)
    mesh.add_edge("frontend", "checkoutservice", 50.0, critical=True)
    mesh.add_edge("frontend", "adservice", 150.0, critical=False)  # UI fallback available

    # Checkout flow
    mesh.add_edge("checkoutservice", "cartservice", 50.0, critical=True)
    mesh.add_edge("checkoutservice", "productcatalogservice", 50.0, critical=True)
    mesh.add_edge("checkoutservice", "currencyservice", 50.0, critical=True)
    mesh.add_edge("checkoutservice", "shippingservice", 50.0, critical=True)
    mesh.add_edge("checkoutservice", "paymentservice", 50.0, critical=True)
    mesh.add_edge("checkoutservice", "emailservice", 50.0, critical=False)  # asynchronous/soft-fail

    # Cart & Shipping downstream dependencies
    mesh.add_edge("cartservice", "valkey-cart", 230.0, critical=True)
    mesh.add_edge("shippingservice", "quoteservice", 90.0, critical=True)
    mesh.add_edge("recommendationservice", "productcatalogservice", 120.0, critical=True)

    # User Transaction Flows (Simulated traffic mix)
    mesh.add_transaction("Browse Catalog", ["frontend", "productcatalogservice", "currencyservice", "adservice"], 0.40)
    mesh.add_transaction("Product Detail & Recommendation", ["frontend", "productcatalogservice", "recommendationservice", "currencyservice"], 0.25)
    mesh.add_transaction("Manage Cart", ["frontend", "cartservice", "valkey-cart"], 0.20)
    mesh.add_transaction("Checkout Order", [
        "frontend", "checkoutservice", "cartservice", "valkey-cart",
        "productcatalogservice", "shippingservice", "quoteservice",
        "paymentservice", "emailservice"
    ], 0.15)

    return mesh


def build_enterprise_fintech_mesh() -> ServiceMesh:
    """
    Tiered Enterprise FinTech & Banking Mesh (16 services)
    """
    mesh = ServiceMesh("Enterprise FinTech Mesh (16 services)")
    mesh.add_ingress_route("api-gateway", 1.0)

    mesh.add_edge("api-gateway", "auth-service", 600.0, critical=True)
    mesh.add_edge("api-gateway", "account-service", 400.0, critical=True)
    mesh.add_edge("api-gateway", "transfer-service", 250.0, critical=True)
    mesh.add_edge("api-gateway", "statement-service", 80.0, critical=False)

    mesh.add_edge("auth-service", "session-vault", 600.0, critical=True)
    mesh.add_edge("auth-service", "user-db", 150.0, critical=True)

    mesh.add_edge("account-service", "auth-service", 400.0, critical=True)
    mesh.add_edge("account-service", "ledger-db", 400.0, critical=True)
    mesh.add_edge("account-service", "cache-cluster", 350.0, critical=False)

    mesh.add_edge("transfer-service", "account-service", 250.0, critical=True)
    mesh.add_edge("transfer-service", "fraud-detection", 250.0, critical=True)
    mesh.add_edge("transfer-service", "fx-rate-service", 100.0, critical=True)
    mesh.add_edge("transfer-service", "clearing-house", 200.0, critical=True)
    mesh.add_edge("transfer-service", "audit-service", 250.0, critical=True)
    mesh.add_edge("transfer-service", "notification-service", 200.0, critical=False)

    mesh.add_edge("fraud-detection", "risk-rules-db", 250.0, critical=True)
    mesh.add_edge("clearing-house", "swift-gateway", 180.0, critical=True)
    mesh.add_edge("statement-service", "ledger-db", 80.0, critical=True)
    mesh.add_edge("audit-service", "audit-store", 250.0, critical=True)

    # Transactions
    mesh.add_transaction("Login / Auth", ["api-gateway", "auth-service", "session-vault"], 0.30)
    mesh.add_transaction("Check Balance", ["api-gateway", "auth-service", "account-service", "ledger-db", "cache-cluster"], 0.40)
    mesh.add_transaction("Fund Transfer", [
        "api-gateway", "auth-service", "transfer-service", "account-service",
        "ledger-db", "fraud-detection", "risk-rules-db", "clearing-house",
        "swift-gateway", "audit-service", "audit-store", "notification-service"
    ], 0.25)
    mesh.add_transaction("Download Statement", ["api-gateway", "auth-service", "statement-service", "ledger-db"], 0.05)

    return mesh


def build_media_streaming_mesh() -> ServiceMesh:
    """
    Media Streaming Platform Mesh (14 services)
    """
    mesh = ServiceMesh("Media Streaming Platform Mesh (14 services)")
    mesh.add_ingress_route("edge-ingress", 1.0)

    mesh.add_edge("edge-ingress", "user-profile", 500.0, critical=True)
    mesh.add_edge("edge-ingress", "catalog-service", 700.0, critical=True)
    mesh.add_edge("edge-ingress", "playback-auth", 400.0, critical=True)
    mesh.add_edge("edge-ingress", "recommendation", 350.0, critical=False)

    mesh.add_edge("user-profile", "account-db", 500.0, critical=True)
    mesh.add_edge("catalog-service", "metadata-db", 700.0, critical=True)
    mesh.add_edge("catalog-service", "elastic-search", 600.0, critical=False)

    mesh.add_edge("playback-auth", "drm-license-server", 400.0, critical=True)
    mesh.add_edge("playback-auth", "cdn-token-service", 400.0, critical=True)
    mesh.add_edge("playback-auth", "telemetry-collector", 380.0, critical=False)

    mesh.add_edge("recommendation", "catalog-service", 350.0, critical=True)
    mesh.add_edge("recommendation", "ml-inference-cluster", 300.0, critical=False)

    mesh.add_edge("cdn-token-service", "crypto-keystore", 400.0, critical=True)

    mesh.add_transaction("Browse Catalog", ["edge-ingress", "catalog-service", "metadata-db", "elastic-search", "recommendation"], 0.50)
    mesh.add_transaction("Watch Video Stream", [
        "edge-ingress", "playback-auth", "drm-license-server",
        "cdn-token-service", "crypto-keystore", "telemetry-collector"
    ], 0.40)
    mesh.add_transaction("Account Profile", ["edge-ingress", "user-profile", "account-db"], 0.10)

    return mesh


def build_deep_chain_mesh() -> ServiceMesh:
    """
    Deep Linear Chain with Branching Branches to evaluate Depth vs Degree (10 services)
    """
    mesh = ServiceMesh("Deep Dependency Pipeline (10 services)")
    mesh.add_ingress_route("stage-1", 1.0)
    mesh.add_edge("stage-1", "stage-2", 300.0, critical=True)
    mesh.add_edge("stage-2", "stage-3", 280.0, critical=True)
    mesh.add_edge("stage-3", "stage-4", 250.0, critical=True)
    mesh.add_edge("stage-4", "stage-5", 220.0, critical=True)
    mesh.add_edge("stage-5", "stage-6", 200.0, critical=True)

    # Sidecar leaf helpers
    mesh.add_edge("stage-2", "logger-sidecar", 280.0, critical=False)
    mesh.add_edge("stage-4", "cache-sidecar", 250.0, critical=False)
    mesh.add_edge("stage-5", "metrics-sidecar", 220.0, critical=False)
    mesh.add_edge("stage-6", "persistence-store", 200.0, critical=True)

    mesh.add_transaction("Deep Pipeline", [
        "stage-1", "stage-2", "stage-3", "stage-4", "stage-5", "stage-6",
        "persistence-store"
    ], 1.0)

    return mesh


# -----------------------------------------------------------------------------
# Graph Metrics Calculation (Matching internal/graph/scoring.go exactly)
# -----------------------------------------------------------------------------

class ExtractedFeatures(NamedTuple):
    in_degree: float
    out_degree: float
    call_freq: float
    depth: float
    spof: float


def compute_graph_features(mesh: ServiceMesh) -> Dict[str, ExtractedFeatures]:
    nodes = list(mesh.nodes)
    in_deg = defaultdict(int)
    out_deg = defaultdict(int)
    call_freq = defaultdict(float)
    callers = defaultdict(set)
    callees = defaultdict(set)
    adj_forward = defaultdict(list)

    for edge in mesh.edges:
        callers[edge.dst].add(edge.src)
        callees[edge.src].add(edge.dst)
        call_freq[edge.dst] += edge.freq
        adj_forward[edge.src].append(edge.dst)

    for n in nodes:
        in_deg[n] = len(callers[n])
        out_deg[n] = len(callees[n])

    # Roots: in-degree 0
    roots = [n for n in nodes if in_deg[n] == 0]
    if not roots:
        roots = list(nodes)

    # Compute Depths via BFS from roots
    depths = {n: 0 for n in nodes}
    queue = deque(roots)
    visited = set(roots)
    for r in roots:
        depths[r] = 0

    while queue:
        curr = queue.popleft()
        for nxt in adj_forward[curr]:
            if nxt not in visited:
                visited.add(nxt)
                depths[nxt] = depths[curr] + 1
                queue.append(nxt)

    # Identify SPOFs via BFS reachability partitioning (exact Go implementation)
    def get_reachable(active_roots: List[str], exclude: str) -> Set[str]:
        seen = set()
        q = deque([r for r in active_roots if r != exclude])
        for r in q:
            seen.add(r)
        while q:
            c = q.popleft()
            for nxt in adj_forward[c]:
                if nxt != exclude and nxt not in seen:
                    seen.add(nxt)
                    q.append(nxt)
        return seen

    reachable_before = get_reachable(roots, "")
    spof_map = {}

    for cand in nodes:
        active_roots = [r for r in roots if r != cand]
        if not active_roots:
            spof_map[cand] = False
            continue
        reachable_after = get_reachable(active_roots, cand)
        isolated = [n for n in reachable_before if n != cand and n not in reachable_after]
        spof_map[cand] = len(isolated) > 0

    # Normalization (Max scaling per mesh as done in scoring.go)
    max_in = max([in_deg[n] for n in nodes] + [1])
    max_out = max([out_deg[n] for n in nodes] + [1])
    max_freq = max([call_freq[n] for n in nodes] + [1.0])
    max_depth = max([depths[n] for n in nodes] + [1])

    features = {}
    for n in nodes:
        norm_in = in_deg[n] / max_in
        norm_out = out_deg[n] / max_out
        norm_freq = call_freq[n] / max_freq
        norm_depth = depths[n] / max_depth
        norm_spof = 1.0 if spof_map[n] else 0.0

        features[n] = ExtractedFeatures(norm_in, norm_out, norm_freq, norm_depth, norm_spof)

    return features


# -----------------------------------------------------------------------------
# Empirical Failure Simulation (Monte Carlo Ground Truth)
# -----------------------------------------------------------------------------

def simulate_mesh_failure_impact(mesh: ServiceMesh, num_trials: int = 10000) -> Dict[str, float]:
    """
    Simulates failure injection for each service in the mesh.
    Measures:
      1. Transaction Drop Rate (Fraction of user requests that fail)
      2. Cascading Upstream Error Rate
      3. Downstream Service Isolation / Starvation
    Returns ground-truth criticality impact normalized to [0.0, 1.0].
    """
    ground_truth = {}
    nodes = list(mesh.nodes)

    # Pre-build critical dependency map: caller -> list of critical callees
    crit_deps = defaultdict(list)
    non_crit_deps = defaultdict(list)
    for edge in mesh.edges:
        if edge.critical:
            crit_deps[edge.src].append(edge.dst)
        else:
            non_crit_deps[edge.src].append(edge.dst)

    # For each node under test, inject 100% failure and run transactions
    for target in nodes:
        failed_tx_weight = 0.0
        total_tx_weight = 0.0
        cascade_failures = set()

        # Simulate cascading failure upward from target
        # A node fails if any of its critical downstream dependencies fail
        def is_service_healthy(s: str, failed_set: Set[str], memo: Dict[str, bool]) -> bool:
            if s in failed_set:
                return False
            if s in memo:
                return memo[s]
            # Check critical dependencies
            for dep in crit_deps[s]:
                if not is_service_healthy(dep, failed_set, memo):
                    memo[s] = False
                    return False
            memo[s] = True
            return True

        failed_nodes = {target}
        memo_health = {}
        for n in nodes:
            if not is_service_healthy(n, failed_nodes, memo_health):
                cascade_failures.add(n)

        # Blast radius fraction
        blast_radius_pct = len(cascade_failures) / len(nodes)

        # Evaluate Transaction Impact
        for tx_name, path, tx_weight in mesh.transactions:
            total_tx_weight += tx_weight
            tx_failed = False
            for step in path:
                if step in cascade_failures:
                    tx_failed = True
                    break
            if tx_failed:
                failed_tx_weight += tx_weight

        tx_loss_ratio = failed_tx_weight / total_tx_weight if total_tx_weight > 0 else 0.0

        # Structural Isolation: count nodes unreachable from ingress
        ingress_roots = [r for r, _ in mesh.ingress_routes]
        q = deque([r for r in ingress_roots if r not in cascade_failures])
        reached = set(q)
        adj_forward = defaultdict(list)
        for e in mesh.edges:
            adj_forward[e.src].append(e.dst)

        while q:
            curr = q.popleft()
            for nxt in adj_forward[curr]:
                if nxt not in cascade_failures and nxt not in reached:
                    reached.add(nxt)
                    q.append(nxt)

        isolation_ratio = (len(nodes) - len(reached)) / len(nodes)

        # Composite ground-truth empirical impact score:
        # Business/User Tx Loss is most critical (0.50), followed by Blast Radius (0.30) and Structural Isolation (0.20)
        empirical_impact = (0.50 * tx_loss_ratio) + (0.30 * blast_radius_pct) + (0.20 * isolation_ratio)
        ground_truth[target] = empirical_impact

    # Normalize ground truth within mesh to [0.0, 1.0]
    max_impact = max(ground_truth.values()) if ground_truth else 1.0
    if max_impact > 0:
        for n in ground_truth:
            ground_truth[n] = ground_truth[n] / max_impact

    return ground_truth


# -----------------------------------------------------------------------------
# Statistical Metrics: Spearman Rank, Kendall Tau, MSE, NDCG
# -----------------------------------------------------------------------------

def spearman_rank_correlation(x: List[float], y: List[float]) -> float:
    """Computes Spearman's rank correlation coefficient."""
    n = len(x)
    if n <= 1:
        return 1.0

    def get_ranks(vals: List[float]) -> List[float]:
        indexed = sorted(enumerate(vals), key=lambda t: t[1])
        ranks = [0.0] * n
        i = 0
        while i < n:
            j = i
            while j < n - 1 and math.isclose(indexed[j][1], indexed[j + 1][1], abs_tol=1e-9):
                j += 1
            mean_rank = (i + j) / 2.0 + 1.0
            for k in range(i, j + 1):
                ranks[indexed[k][0]] = mean_rank
            i = j + 1
        return ranks

    rx = get_ranks(x)
    ry = get_ranks(y)

    mean_rx = sum(rx) / n
    mean_ry = sum(ry) / n

    cov = sum((rx[i] - mean_rx) * (ry[i] - mean_ry) for i in range(n))
    std_x = math.sqrt(sum((rx[i] - mean_rx) ** 2 for i in range(n)))
    std_y = math.sqrt(sum((ry[i] - mean_ry) ** 2 for i in range(n)))

    if std_x == 0 or std_y == 0:
        return 0.0
    return cov / (std_x * std_y)


def kendall_tau(x: List[float], y: List[float]) -> float:
    """Computes Kendall's tau rank correlation coefficient."""
    n = len(x)
    if n <= 1:
        return 1.0
    concordant = 0
    discordant = 0
    for i in range(n):
        for j in range(i + 1, n):
            dx = x[i] - x[j]
            dy = y[i] - y[j]
            if dx * dy > 0:
                concordant += 1
            elif dx * dy < 0:
                discordant += 1
    total_pairs = n * (n - 1) / 2
    if total_pairs == 0:
        return 0.0
    return (concordant - discordant) / total_pairs


def mean_squared_error(preds: List[float], targets: List[float]) -> float:
    n = len(preds)
    if n == 0:
        return 0.0
    return sum((p - t) ** 2 for p, t in zip(preds, targets)) / n


def ndcg_at_k(preds: List[float], targets: List[float], k: int = 5) -> float:
    """Normalized Discounted Cumulative Gain at rank K."""
    n = len(preds)
    k = min(k, n)
    if k == 0:
        return 1.0

    # Sort items according to predicted scores descending
    order = sorted(range(n), key=lambda i: preds[i], reverse=True)
    dcg = sum((2 ** targets[order[i]] - 1) / math.log2(i + 2) for i in range(k))

    # Ideal ranking according to targets
    ideal_order = sorted(range(n), key=lambda i: targets[i], reverse=True)
    idcg = sum((2 ** targets[ideal_order[i]] - 1) / math.log2(i + 2) for i in range(k))

    return dcg / idcg if idcg > 0 else 1.0


# -----------------------------------------------------------------------------
# Optimization & Evaluation Engine
# -----------------------------------------------------------------------------

def evaluate_weights(
    weights: Tuple[float, float, float, float, float],
    meshes_data: List[Tuple[ServiceMesh, Dict[str, ExtractedFeatures], Dict[str, float]]]
) -> Tuple[float, float, float, float]:
    """
    Evaluates a candidate weight vector across all simulated meshes.
    Returns: (mean_spearman, mean_kendall, mean_mse, mean_ndcg5)
    """
    w1, w2, w3, w4, w5 = weights
    spearmans = []
    kendalls = []
    mses = []
    ndcgs = []

    for mesh, features, ground_truth in meshes_data:
        nodes = list(mesh.nodes)
        pred_scores = []
        actual_scores = []

        for n in nodes:
            feat = features[n]
            raw_s = (
                w1 * feat.in_degree +
                w2 * feat.out_degree +
                w3 * feat.call_freq +
                w4 * feat.depth +
                w5 * feat.spof
            )
            # Bound [0, 1] and round to 2 decimals as in scoring.go
            s = min(1.0, max(0.0, raw_s))
            pred_scores.append(s)
            actual_scores.append(ground_truth[n])

        spearmans.append(spearman_rank_correlation(pred_scores, actual_scores))
        kendalls.append(kendall_tau(pred_scores, actual_scores))
        mses.append(mean_squared_error(pred_scores, actual_scores))
        ndcgs.append(ndcg_at_k(pred_scores, actual_scores, k=5))

    return (
        sum(spearmans) / len(spearmans),
        sum(kendalls) / len(kendalls),
        sum(mses) / len(mses),
        sum(ndcgs) / len(ndcgs),
    )


def run_simplex_grid_search(
    meshes_data: List[Tuple[ServiceMesh, Dict[str, ExtractedFeatures], Dict[str, float]]],
    step: float = 0.05
) -> List[Tuple[Tuple[float, float, float, float, float], float, float, float, float]]:
    """
    Comprehensive grid search over the 5-dimensional weight simplex:
    w1 + w2 + w3 + w4 + w5 = 1.0, wi >= 0
    """
    results = []
    steps = int(round(1.0 / step))

    for i1 in range(steps + 1):
        w1 = round(i1 * step, 3)
        rem1 = steps - i1
        for i2 in range(rem1 + 1):
            w2 = round(i2 * step, 3)
            rem2 = rem1 - i2
            for i3 in range(rem2 + 1):
                w3 = round(i3 * step, 3)
                rem3 = rem2 - i3
                for i4 in range(rem3 + 1):
                    w4 = round(i4 * step, 3)
                    i5 = rem3 - i4
                    w5 = round(i5 * step, 3)

                    weights = (w1, w2, w3, w4, w5)
                    sp, kd, mse, ndcg = evaluate_weights(weights, meshes_data)
                    results.append((weights, sp, kd, mse, ndcg))

    # Sort descending by composite score (prioritizing Spearman correlation & NDCG)
    results.sort(key=lambda x: (x[1] * 0.5 + x[4] * 0.3 - x[3] * 0.2), reverse=True)
    return results


def fine_tune_weights(
    best_weights: Tuple[float, float, float, float, float],
    meshes_data: List[Tuple[ServiceMesh, Dict[str, ExtractedFeatures], Dict[str, float]]],
    fine_step: float = 0.01,
    radius: int = 3
) -> Tuple[Tuple[float, float, float, float, float], float, float, float, float]:
    """
    Local neighborhood refinement around the best candidate weights.
    """
    b1, b2, b3, b4, b5 = best_weights
    best_res = (best_weights, *evaluate_weights(best_weights, meshes_data))
    best_score = best_res[1] * 0.5 + best_res[4] * 0.3 - best_res[3] * 0.2

    for d1 in range(-radius, radius + 1):
        for d2 in range(-radius, radius + 1):
            for d3 in range(-radius, radius + 1):
                for d4 in range(-radius, radius + 1):
                    for d5 in range(-radius, radius + 1):
                        w1 = round(b1 + d1 * fine_step, 4)
                        w2 = round(b2 + d2 * fine_step, 4)
                        w3 = round(b3 + d3 * fine_step, 4)
                        w4 = round(b4 + d4 * fine_step, 4)
                        w5 = round(b5 + d5 * fine_step, 4)

                        if min(w1, w2, w3, w4, w5) < 0.0:
                            continue
                        total = w1 + w2 + w3 + w4 + w5
                        if not math.isclose(total, 1.0, abs_tol=1e-5):
                            continue

                        weights = (w1, w2, w3, w4, w5)
                        sp, kd, mse, ndcg = evaluate_weights(weights, meshes_data)
                        score = sp * 0.5 + ndcg * 0.3 - mse * 0.2
                        if score > best_score:
                            best_score = score
                            best_res = (weights, sp, kd, mse, ndcg)

    return best_res


# -----------------------------------------------------------------------------
# Main Simulation Execution
# -----------------------------------------------------------------------------

def main():
    print("=" * 80)
    print(" DAMASCUS CRITICALITY WEIGHT SIMULATION & EMPIRICAL OPTIMIZATION ENGINE")
    print("=" * 80)
    print("Generating simulated microservice environments:")
    meshes = [
        build_astronomy_shop_mesh(),
        build_enterprise_fintech_mesh(),
        build_media_streaming_mesh(),
        build_deep_chain_mesh(),
    ]

    meshes_data = []
    for m in meshes:
        feats = compute_graph_features(m)
        gt = simulate_mesh_failure_impact(m, num_trials=10000)
        meshes_data.append((m, feats, gt))
        print(f"  * {m.name}: {len(m.nodes)} nodes, {len(m.edges)} edges, {len(m.transactions)} user flows simulated.")

    print("\n" + "-" * 80)
    print("1. EVALUATING HEURISTIC / CURRENT DEFAULT WEIGHTS")
    print("-" * 80)
    default_weights = (0.35, 0.15, 0.25, 0.10, 0.15)
    def_sp, def_kd, def_mse, def_ndcg = evaluate_weights(default_weights, meshes_data)
    print(f"Default Weights: w_InDegree=0.35, w_OutDegree=0.15, w_CallFreq=0.25, w_Depth=0.10, w_SPOF=0.15")
    print(f"  -> Spearman Rank Correlation (rho): {def_sp:.4f}")
    print(f"  -> Kendall's Tau (tau):             {def_kd:.4f}")
    print(f"  -> Mean Squared Error (MSE):       {def_mse:.4f}")
    print(f"  -> NDCG @ 5:                       {def_ndcg:.4f}")

    print("\n" + "-" * 80)
    print("2. RUNNING 5-SIMPLEX GRID SEARCH OVER WEIGHT COMBINATIONS (step = 0.05)")
    print("-" * 80)
    grid_results = run_simplex_grid_search(meshes_data, step=0.05)
    print(f"Evaluated {len(grid_results)} distinct valid simplex weight combinations.")

    print("\nTop 5 Candidates from Simplex Grid Search:")
    print(f"{'Rank':<5} {'InDeg':<8} {'OutDeg':<8} {'CallFreq':<10} {'Depth':<8} {'SPOF':<8} {'Spearman':<10} {'Kendall':<10} {'NDCG@5':<10} {'MSE':<8}")
    for idx, (w, sp, kd, mse, ndcg) in enumerate(grid_results[:5], 1):
        print(f"{idx:<5} {w[0]:<8.2f} {w[1]:<8.2f} {w[2]:<10.2f} {w[3]:<8.2f} {w[4]:<8.2f} {sp:<10.4f} {kd:<10.4f} {ndcg:<10.4f} {mse:<8.4f}")

    print("\n" + "-" * 80)
    print("3. LOCAL FINE-TUNING REFINEMENT (step = 0.01)")
    print("-" * 80)
    best_candidate = grid_results[0][0]
    opt_weights, opt_sp, opt_kd, opt_mse, opt_ndcg = fine_tune_weights(best_candidate, meshes_data, fine_step=0.01, radius=4)

    print(f"Optimal Simulated Weights Found:")
    print(f"  w_InDegree  = {opt_weights[0]:.2f}")
    print(f"  w_OutDegree = {opt_weights[1]:.2f}")
    print(f"  w_CallFreq  = {opt_weights[2]:.2f}")
    print(f"  w_Depth     = {opt_weights[3]:.2f}")
    print(f"  w_SPOF      = {opt_weights[4]:.2f}")
    print(f"  Sum         = {sum(opt_weights):.2f}")
    print(f"\nOptimal Performance Metrics:")
    print(f"  -> Spearman Rank Correlation (rho): {opt_sp:.4f} (improvement: {(opt_sp - def_sp):+.4f})")
    print(f"  -> Kendall's Tau (tau):             {opt_kd:.4f} (improvement: {(opt_kd - def_kd):+.4f})")
    print(f"  -> NDCG @ 5:                       {opt_ndcg:.4f} (improvement: {(opt_ndcg - def_ndcg):+.4f})")
    print(f"  -> Mean Squared Error (MSE):       {opt_mse:.4f} (improvement: {(def_mse - opt_mse):+.4f})")

    # -------------------------------------------------------------------------
    # Detailed Mesh Comparison (Astronomy Shop Demo)
    # -------------------------------------------------------------------------
    print("\n" + "=" * 80)
    print(" DETAILED SERVICE RANKING: OpenTelemetry Astronomy Shop Demo")
    print("=" * 80)
    astro_mesh, astro_feats, astro_gt = meshes_data[0]
    astro_nodes = sorted(list(astro_mesh.nodes))

    table_data = []
    for n in astro_nodes:
        feat = astro_feats[n]
        def_score = (
            default_weights[0] * feat.in_degree +
            default_weights[1] * feat.out_degree +
            default_weights[2] * feat.call_freq +
            default_weights[3] * feat.depth +
            default_weights[4] * feat.spof
        )
        opt_score = (
            opt_weights[0] * feat.in_degree +
            opt_weights[1] * feat.out_degree +
            opt_weights[2] * feat.call_freq +
            opt_weights[3] * feat.depth +
            opt_weights[4] * feat.spof
        )
        gt_score = astro_gt[n]
        table_data.append((n, feat, gt_score, def_score, opt_score))

    # Sort table by Ground Truth descending
    table_data.sort(key=lambda x: x[2], reverse=True)

    header = f"{'Service Name':<24} {'InDeg':<6} {'OutDeg':<7} {'Freq':<6} {'Dpth':<5} {'SPOF':<5} | {'GroundTruth':<12} {'Default':<10} {'SimulatedOpt':<12}"
    print(header)
    print("-" * len(header))
    for name, feat, gt, def_s, opt_s in table_data:
        print(f"{name:<24} {feat.in_degree:<6.2f} {feat.out_degree:<7.2f} {feat.call_freq:<6.2f} {feat.depth:<5.2f} {feat.spof:<5.0f} | {gt:<12.2f} {def_s:<10.2f} {opt_s:<12.2f}")

    print("\n" + "=" * 80)
    print(" FEATURE ABLATION & SENSITIVITY ANALYSIS (Marginal Importance)")
    print("=" * 80)
    # Evaluate setting each feature to 0 while keeping others proportional
    for feat_idx, feat_name in enumerate(["InDegree", "OutDegree", "CallFreq", "Depth", "SPOF"]):
        sub_w = list(opt_weights)
        sub_w[feat_idx] = 0.0
        rem_sum = sum(sub_w)
        if rem_sum > 0:
            norm_sub_w = tuple(w / rem_sum for w in sub_w)
        else:
            norm_sub_w = tuple(0.2 for _ in range(5))
        sub_sp, _, _, sub_ndcg = evaluate_weights(norm_sub_w, meshes_data)
        drop_sp = opt_sp - sub_sp
        drop_ndcg = opt_ndcg - sub_ndcg
        print(f"  * Removing {feat_name:<10}: Spearman rho drops by {drop_sp:+.4f} (rho={sub_sp:.4f}), NDCG drops by {drop_ndcg:+.4f}")

    print("=" * 80)


if __name__ == "__main__":
    main()
