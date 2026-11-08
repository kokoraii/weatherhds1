//! Turns polygon rings into border lines. Keeps only edges two polygons share, each once: drawing polygon
//! outlines instead would double every border and trace the whole coastline.

use std::collections::{HashMap, HashSet};

/// [longitude, latitude] in degrees.
pub type Point = [f64; 2];
/// A position in web-mercator world coordinates: (0,0) is the north-west corner, (1,1) the south-east.
pub type WorldPoint = [f64; 2];

/// Vertices closer than this (about 10 cm) count as the same vertex.
const SNAP_PER_DEGREE: f64 = 1e6;
const MERCATOR_MAX_LATITUDE: f64 = 85.051_128;
/// Grid size for finding nearby boundaries, about 3 km. Checking the cells around an edge's own gives a
/// reach of 3 to 6 km, which covers how far independently simplified neighbours drift apart.
const NEARBY_CELL_DEGREES: f64 = 0.03;

type Node = (i32, i32);
type Cell = (i32, i32);

fn snap(point: Point) -> Node {
    ((point[0] * SNAP_PER_DEGREE).round() as i32, (point[1] * SNAP_PER_DEGREE).round() as i32)
}

fn to_world(node: Node) -> WorldPoint {
    let lon = f64::from(node.0) / SNAP_PER_DEGREE;
    let lat = (f64::from(node.1) / SNAP_PER_DEGREE).clamp(-MERCATOR_MAX_LATITUDE, MERCATOR_MAX_LATITUDE);
    let sin = lat.to_radians().sin();
    [(lon + 180.0) / 360.0, 0.5 - ((1.0 + sin) / (1.0 - sin)).ln() / (4.0 * std::f64::consts::PI)]
}

/// The shared edges of `rings`, joined into lines that run between junctions, plus how many edges were
/// found in all and how many were shared (for judging how clean a source's topology is).
pub fn shared_lines(rings: &[Vec<Point>]) -> (Vec<Vec<WorldPoint>>, usize, usize) {
    let mut edge_uses: HashMap<(Node, Node), u32> = HashMap::new();
    for ring in rings {
        let mut previous: Option<Node> = None;
        for &point in ring {
            let node = snap(point);
            if let Some(last) = previous.filter(|&last| last != node) {
                *edge_uses.entry((last.min(node), last.max(node))).or_default() += 1;
            }
            previous = Some(node);
        }
    }

    let total = edge_uses.len();
    let mut neighbours: HashMap<Node, Vec<Node>> = HashMap::new();
    let mut shared = 0;
    for (&(a, b), &uses) in &edge_uses {
        if uses >= 2 {
            neighbours.entry(a).or_default().push(b);
            neighbours.entry(b).or_default().push(a);
            shared += 1;
        }
    }
    drop(edge_uses);
    (walk_lines(&neighbours), total, shared)
}

/// Border lines for a source whose neighbouring polygons were simplified independently, so they don't share
/// vertices and `shared_lines` would find almost nothing. An edge counts as a border if a different polygon's
/// boundary runs within a few kilometres of it, and only the polygon with the lower id keeps it, which gives
/// one line per border and still drops coastlines. `rings` holds (polygon id, ring).
pub fn nearby_lines(rings: &[(u32, Vec<Point>)]) -> (Vec<Vec<WorldPoint>>, usize, usize) {
    // The highest polygon id with boundary in each grid cell.
    let mut highest: HashMap<Cell, u32> = HashMap::new();
    for (polygon, ring) in rings {
        for pair in ring.windows(2) {
            for cell in cells_along(pair[0], pair[1]) {
                let entry = highest.entry(cell).or_insert(*polygon);
                *entry = (*entry).max(*polygon);
            }
        }
    }

    let mut edges: HashSet<(Node, Node)> = HashSet::new();
    let mut total = 0;
    for (polygon, ring) in rings {
        for pair in ring.windows(2) {
            let (a, b) = (snap(pair[0]), snap(pair[1]));
            if a == b {
                continue;
            }
            total += 1;
            let cells = cells_along(pair[0], pair[1]);
            let near_higher = cells
                .iter()
                .filter(|cell| {
                    (-1..=1).any(|dy| {
                        (-1..=1).any(|dx| highest.get(&(cell.0 + dx, cell.1 + dy)).is_some_and(|id| id > polygon))
                    })
                })
                .count();
            // Most of the edge, so a coastline doesn't get kept just for ending at a border.
            if near_higher * 2 > cells.len() {
                edges.insert((a.min(b), a.max(b)));
            }
        }
    }

    let kept = edges.len();
    let mut neighbours: HashMap<Node, Vec<Node>> = HashMap::new();
    for (a, b) in edges {
        neighbours.entry(a).or_default().push(b);
        neighbours.entry(b).or_default().push(a);
    }
    (walk_lines(&neighbours), total, kept)
}

fn cell_of(point: Point) -> Cell {
    ((point[0] / NEARBY_CELL_DEGREES).floor() as i32, (point[1] / NEARBY_CELL_DEGREES).floor() as i32)
}

/// The grid cells a segment passes through, sampled at the cell size.
fn cells_along(a: Point, b: Point) -> Vec<Cell> {
    let length = (b[0] - a[0]).abs().max((b[1] - a[1]).abs());
    let steps = (length / NEARBY_CELL_DEGREES).ceil().max(1.0) as usize;
    let mut cells: Vec<Cell> = (0..=steps)
        .map(|i| {
            let t = i as f64 / steps as f64;
            cell_of([a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t])
        })
        .collect();
    cells.dedup();
    cells
}

/// Follows the edge graph into lines: from each junction or dead end out to the next, then any closed
/// loops that remain. Each edge lands in exactly one line.
fn walk_lines(neighbours: &HashMap<Node, Vec<Node>>) -> Vec<Vec<WorldPoint>> {
    let is_junction = |node: &Node| neighbours[node].len() != 2;
    let mut walked: HashMap<(Node, Node), bool> = HashMap::new();
    let mut lines = Vec::new();

    let walk = |start: Node, first: Node, walked: &mut HashMap<(Node, Node), bool>| {
        let mut line = vec![to_world(start)];
        let (mut previous, mut current) = (start, first);
        walked.insert((previous.min(current), previous.max(current)), true);
        loop {
            line.push(to_world(current));
            if current == start || is_junction(&current) {
                break;
            }
            let next = *neighbours[&current]
                .iter()
                .find(|&&candidate| candidate != previous)
                .expect("a node of degree 2 has another neighbour");
            walked.insert((current.min(next), current.max(next)), true);
            previous = current;
            current = next;
        }
        line
    };

    // Sorted so a build's output doesn't depend on HashMap order.
    let mut nodes: Vec<Node> = neighbours.keys().copied().collect();
    nodes.sort_unstable();

    for &node in nodes.iter().filter(|node| is_junction(node)) {
        for &next in &neighbours[&node] {
            if !walked.contains_key(&(node.min(next), node.max(next))) {
                lines.push(walk(node, next, &mut walked));
            }
        }
    }
    for &node in &nodes {
        for &next in &neighbours[&node] {
            if !walked.contains_key(&(node.min(next), node.max(next))) {
                lines.push(walk(node, next, &mut walked));
            }
        }
    }
    lines
}

/// Douglas-Peucker simplification, keeping the line's ends so junctions stay joined.
pub fn simplify(line: &[WorldPoint], tolerance: f64) -> Vec<WorldPoint> {
    let mut keep = vec![false; line.len()];
    keep[0] = true;
    keep[line.len() - 1] = true;
    let mut stack = vec![(0, line.len() - 1)];
    // A closed loop has no chord to measure from, so it is seeded with its farthest point.
    if line.first() == line.last() && line.len() > 3 {
        let far = (1..line.len() - 1)
            .max_by(|&a, &b| distance_squared(line[0], line[a]).total_cmp(&distance_squared(line[0], line[b])))
            .expect("a loop has interior points");
        keep[far] = true;
        stack = vec![(0, far), (far, line.len() - 1)];
    }
    let limit = tolerance * tolerance;
    while let Some((start, end)) = stack.pop() {
        if end <= start + 1 {
            continue;
        }
        let (farthest, distance) = (start + 1..end)
            .map(|i| (i, segment_distance_squared(line[i], line[start], line[end])))
            .max_by(|a, b| a.1.total_cmp(&b.1))
            .expect("the range is not empty");
        if distance > limit {
            keep[farthest] = true;
            stack.push((start, farthest));
            stack.push((farthest, end));
        }
    }
    line.iter().zip(keep).filter(|(_, kept)| *kept).map(|(point, _)| *point).collect()
}

fn distance_squared(a: WorldPoint, b: WorldPoint) -> f64 {
    (a[0] - b[0]).powi(2) + (a[1] - b[1]).powi(2)
}

fn segment_distance_squared(point: WorldPoint, a: WorldPoint, b: WorldPoint) -> f64 {
    let (dx, dy) = (b[0] - a[0], b[1] - a[1]);
    let length = dx * dx + dy * dy;
    if length == 0.0 {
        return distance_squared(point, a);
    }
    let along = (((point[0] - a[0]) * dx + (point[1] - a[1]) * dy) / length).clamp(0.0, 1.0);
    distance_squared(point, [a[0] + along * dx, a[1] + along * dy])
}

#[cfg(test)]
mod tests {
    use super::*;

    fn square(x: f64, y: f64) -> Vec<Point> {
        vec![[x, y], [x + 1.0, y], [x + 1.0, y + 1.0], [x, y + 1.0], [x, y]]
    }

    #[test]
    fn two_neighbours_yield_only_their_shared_edge() {
        let (lines, total, shared) = shared_lines(&[square(0.0, 0.0), square(1.0, 0.0)]);
        assert_eq!((total, shared), (7, 1));
        assert_eq!(lines.len(), 1);
        assert_eq!(lines[0].len(), 2);
        let expected = [to_world(snap([1.0, 0.0])), to_world(snap([1.0, 1.0]))];
        assert!(lines[0].contains(&expected[0]) && lines[0].contains(&expected[1]));
    }

    #[test]
    fn a_lone_polygon_is_all_coastline_and_yields_nothing() {
        assert!(shared_lines(&[square(0.0, 0.0)]).0.is_empty());
    }

    #[test]
    fn a_shared_run_of_edges_becomes_one_line() {
        // Both polygons share the run from (1,0) through (1,0.5) to (1,1).
        let left = vec![[0.0, 0.0], [1.0, 0.0], [1.0, 0.5], [1.0, 1.0], [0.0, 1.0], [0.0, 0.0]];
        let right = vec![[1.0, 0.0], [2.0, 0.0], [2.0, 1.0], [1.0, 1.0], [1.0, 0.5], [1.0, 0.0]];
        let (lines, _, _) = shared_lines(&[left, right]);
        assert_eq!(lines.len(), 1);
        assert_eq!(lines[0].len(), 3);
    }

    #[test]
    fn an_enclosed_polygon_yields_a_closed_loop() {
        let inner = square(1.0, 1.0);
        let mut hole = inner.clone();
        hole.reverse();
        let (lines, _, _) = shared_lines(&[inner, hole]);
        assert_eq!(lines.len(), 1);
        assert_eq!(lines[0].len(), 5);
        assert_eq!(lines[0].first(), lines[0].last());
    }

    /// A rectangle spanning x0..x1, y 0..1, with its long sides cut into short edges.
    fn strip(x0: f64, x1: f64) -> Vec<Point> {
        let mut ring = vec![[x0, 0.0]];
        for i in 1..=10 {
            ring.push([x1, f64::from(i) / 10.0]);
        }
        ring.push([x0, 1.0]);
        ring.push([x0, 0.0]);
        ring.insert(1, [x1, 0.0]);
        ring
    }

    #[test]
    fn neighbours_that_almost_touch_yield_one_side_of_their_border() {
        // Their facing edges sit 0.01 degrees apart and share no vertices.
        let (left, right) = (strip(0.0, 1.0), strip(2.0, 1.01));
        let (lines, _, _) = nearby_lines(&[(0, left), (1, right)]);
        let xs: Vec<f64> = lines.iter().flatten().map(|p| p[0]).collect();
        let border_x = to_world(snap([1.0, 0.0]))[0];
        assert!(!xs.is_empty());
        assert!(xs.iter().all(|x| (x - border_x).abs() < 1e-9), "kept more than the left polygon's border: {xs:?}");
    }

    #[test]
    fn nearby_lines_drops_the_coast_of_a_polygon_with_no_neighbours() {
        assert!(nearby_lines(&[(0, strip(0.0, 1.0))]).0.is_empty());
    }

    #[test]
    fn islands_of_one_polygon_do_not_count_as_each_others_neighbours() {
        let (lines, _, _) = nearby_lines(&[(4, strip(0.0, 1.0)), (4, strip(2.0, 1.01))]);
        assert!(lines.is_empty());
    }

    #[test]
    fn a_long_coast_ending_at_a_border_is_not_kept() {
        // A long edge whose far end alone touches the neighbour.
        let coast = vec![[0.0, 0.0], [5.0, 0.0], [5.0, 5.0], [0.0, 0.0]];
        let neighbour = vec![[5.01, -0.1], [6.0, -0.1], [6.0, 0.1], [5.01, 0.1], [5.01, -0.1]];
        let (lines, _, _) = nearby_lines(&[(0, coast), (1, neighbour)]);
        let bottom_y = to_world(snap([0.0, 0.0]))[1];
        let far_west = to_world(snap([0.0, 0.0]))[0];
        assert!(!lines.iter().flatten().any(|p| (p[1] - bottom_y).abs() < 1e-9 && (p[0] - far_west).abs() < 1e-9));
    }

    #[test]
    fn simplifying_keeps_ends_and_real_corners_and_drops_near_straight_points() {
        let line = [[0.0, 0.0], [0.5, 0.0001], [1.0, 0.0], [1.0, 1.0]];
        assert_eq!(simplify(&line, 0.01), vec![[0.0, 0.0], [1.0, 0.0], [1.0, 1.0]]);
    }

    #[test]
    fn simplifying_a_closed_loop_keeps_it_a_loop() {
        let simplified = simplify(&[[0.0, 0.0], [1.0, 0.0], [1.0, 1.0], [0.0, 1.0], [0.0, 0.0]], 0.01);
        assert_eq!(simplified.first(), simplified.last());
        assert!(simplified.len() >= 4);
    }

    #[test]
    fn world_coordinates_put_the_equator_and_prime_meridian_in_the_middle() {
        let [x, y] = to_world(snap([0.0, 0.0]));
        assert!((x - 0.5).abs() < 1e-9 && (y - 0.5).abs() < 1e-9);
    }
}
