"""Make SVTR attention 4-D for the TFLite GPU delegate:
qkv[B,N,3*H*D] -Reshape[B,N,3,H,D]-Transpose(2,0,3,1,4)-Slice(axis0,i)-Squeeze(0)  ==>  Split(last,3)[i]-Reshape[B,N,H,D]-Transpose(0,2,1,3)
and drop no-op Unsqueeze->Squeeze pairs. Usage: fix_attn.py IN OUT"""
import sys, onnx, numpy as np
from onnx import helper, numpy_helper, shape_inference
m = shape_inference.infer_shapes(onnx.load(sys.argv[1])); g = m.graph
init = {i.name: numpy_helper.to_array(i) for i in g.initializer}
sh = {v.name: [d.dim_value for d in v.type.tensor_type.shape.dim] for v in list(g.value_info) + list(g.input) + list(g.output)}
cons = {}
for n in g.node:
    for i in n.input: cons.setdefault(i, []).append(n)
const = lambda name: init[name] if name in init else next(numpy_helper.to_array(a.t) for n in g.node if n.op_type == 'Constant' and n.output[0] == name for a in n.attribute)
drop, add, rename = set(), [], {}
for n in g.node:
    if n.op_type == 'Reshape' and len(sh.get(n.output[0], [])) == 5:
        B, N, three, H, D = sh[n.output[0]]; t = cons[n.output[0]][0]
        assert three == 3 and t.op_type == 'Transpose' and list(t.attribute[0].ints) == [2, 0, 3, 1, 4]
        p = n.name.replace('.', '_'); outs = [f'{p}_q', f'{p}_k', f'{p}_v']
        opset = next(o.version for o in m.opset_import if o.domain in ('', 'ai.onnx'))
        if opset >= 13:
            g.initializer.append(numpy_helper.from_array(np.array([H * D] * 3, np.int64), f'{p}_split'))
        g.initializer.append(numpy_helper.from_array(np.array([B, N, H, D], np.int64), f'{p}_shape'))
        add.append(helper.make_node('Split', [n.input[0], f'{p}_split'], outs, axis=2, name=f'{p}_Split') if opset >= 13 else
                   helper.make_node('Split', [n.input[0]], outs, axis=2, split=[H * D] * 3, name=f'{p}_Split'))
        drop |= {id(n), id(t)}
        for s in cons[t.output[0]]:
            assert s.op_type == 'Slice'; idx = int(const(s.input[1])[0]); sq = cons[s.output[0]][0]; assert sq.op_type == 'Squeeze'
            add += [helper.make_node('Reshape', [outs[idx], f'{p}_shape'], [f'{p}_r{idx}'], name=f'{p}_Reshape{idx}'),
                    helper.make_node('Transpose', [f'{p}_r{idx}'], [sq.output[0]], perm=[0, 2, 1, 3], name=f'{p}_Transpose{idx}')]
            drop |= {id(s), id(sq)}
    if n.op_type == 'Unsqueeze' and len(sh.get(n.output[0], [])) == 5:
        c = cons[n.output[0]]
        if len(c) == 1 and c[0].op_type == 'Squeeze' and sh[c[0].output[0]] == sh[n.input[0]]:
            rename[c[0].output[0]] = n.input[0]; drop |= {id(n), id(c[0])}
keep = [n for n in g.node if id(n) not in drop] + add
for n in keep:
    for j, i in enumerate(n.input):
        if i in rename: n.input[j] = rename[i]
# topological sort
avail = {i.name for i in g.input} | {i.name for i in g.initializer}; order = []; pending = keep
while pending:
    rest = []
    for n in pending:
        if all((not i) or i in avail for i in n.input): order.append(n); avail |= set(n.output)
        else: rest.append(n)
    assert len(rest) < len(pending), 'cycle'; pending = rest
del g.node[:]; g.node.extend(order); del g.value_info[:]
onnx.checker.check_model(m); onnx.save(m, sys.argv[2]); print('rewritten', len(drop), 'nodes')
