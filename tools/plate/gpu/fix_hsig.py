"""Rewrite HardSigmoid(alpha,beta)(x) as Clip(x*6a+6b, 0, 6) * (1/6) so TFLite gets RELU6 (GPU-supported)."""
import sys, onnx, numpy as np
from onnx import helper, numpy_helper
m = onnx.load(sys.argv[1]); g = m.graph; out = []; k = 0
for n in g.node:
    if n.op_type != 'HardSigmoid': out.append(n); continue
    a = next((x.f for x in n.attribute if x.name == 'alpha'), 0.2); b = next((x.f for x in n.attribute if x.name == 'beta'), 0.5)
    p = f'hsfix{k}'; k += 1
    for name, val in [('s', 6 * a), ('o', 6 * b), ('lo', 0.), ('hi', 6.), ('r', 1 / 6)]:
        g.initializer.append(numpy_helper.from_array(np.array(val, np.float32), f'{p}_{name}'))
    out += [helper.make_node('Mul', [n.input[0], f'{p}_s'], [f'{p}_m']), helper.make_node('Add', [f'{p}_m', f'{p}_o'], [f'{p}_a']),
            helper.make_node('Clip', [f'{p}_a', f'{p}_lo', f'{p}_hi'], [f'{p}_c']), helper.make_node('Mul', [f'{p}_c', f'{p}_r'], [n.output[0]])]
del g.node[:]; g.node.extend(out); onnx.checker.check_model(m); onnx.save(m, sys.argv[2]); print('replaced', k)
