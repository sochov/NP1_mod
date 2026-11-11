import importlib.util
import pathlib
import unittest

try:
    from google.protobuf import descriptor_pb2
except ImportError:
    descriptor_pb2 = None

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('protocol_inventory', ROOT / 'tools/inventory_np1_protocol.py')
MODULE = importlib.util.module_from_spec(SPEC)
if descriptor_pb2 is not None:
    SPEC.loader.exec_module(MODULE)


@unittest.skipIf(descriptor_pb2 is None, 'Optional host analysis dependency: protobuf')
class DescriptorInventoryTests(unittest.TestCase):
    def fixture(self):
        value = descriptor_pb2.FileDescriptorProto(name='SyntheticSetting.proto', package='Synthetic')
        message = value.message_type.add(name='Request')
        message.field.add(name='enabled', number=1, label=1, type=8)
        return value.SerializeToString()

    def test_bounded_static_schema_only(self):
        data = b'ELF-prefix' + self.fixture() + b'\0unrelated private material'
        results = MODULE.extract(data)
        self.assertEqual(len(results), 1)
        self.assertEqual(results[0]['name'], 'SyntheticSetting.proto')
        self.assertEqual(results[0]['messages'][0]['fields'][0]['name'], 'enabled')
        self.assertNotIn('private material', str(results))

    def test_truncated_and_false_filename_are_rejected(self):
        self.assertEqual(MODULE.extract(self.fixture()[:-3]), [])
        self.assertEqual(MODULE.extract(b'\x0a\x10NotARealSetting.proto\0'), [])
        with self.assertRaises(ValueError):
            MODULE.varint(b'\x80' * 11, 0, 11)

    def test_separate_adjacent_descriptors(self):
        first = self.fixture()
        second = descriptor_pb2.FileDescriptorProto(name='Other.proto', package='Other')
        second.enum_type.add(name='State').value.add(name='STATE_DEFAULT', number=0)
        self.assertEqual(len(MODULE.extract(first + second.SerializeToString() + b'\0')), 2)


if __name__ == '__main__':
    unittest.main()
