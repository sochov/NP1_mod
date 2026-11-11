import importlib.util
import pathlib
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('ap_channel', pathlib.Path(__file__).resolve().parents[1] / 'tools/device/ap-channel.py')
ap_channel = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ap_channel)


class APChannelTests(unittest.TestCase):
    def test_abstract_reply_needs_no_directory_write_or_permission_change(self):
        stream = mock.Mock()
        stream.recv.return_value = b'wpa_state=COMPLETED\nfreq=2437\n'
        with mock.patch.object(ap_channel.socket, 'AF_UNIX', 1, create=True), mock.patch.object(ap_channel.socket, 'socket', return_value=stream), mock.patch.object(ap_channel.os, 'mkdir', side_effect=AssertionError('filesystem write')), mock.patch.object(ap_channel.os, 'chown', side_effect=AssertionError('permission mutation'), create=True):
            status = ap_channel.read_status()
        self.assertEqual(ap_channel.channel(status), 6)
        self.assertTrue(stream.bind.call_args.args[0].startswith('\0ncc-ap-status-'))
        stream.send.assert_called_once_with(b'STATUS')
        stream.close.assert_called_once()

    def test_collision_retries_only_for_address_in_use(self):
        stream = mock.Mock()
        stream.bind.side_effect = [OSError(98, 'in use'), None]
        stream.recv.return_value = b'wpa_state=DISCONNECTED\n'
        with mock.patch.object(ap_channel.socket, 'AF_UNIX', 1, create=True), mock.patch.object(ap_channel.socket, 'socket', return_value=stream):
            self.assertEqual(ap_channel.read_status()['wpa_state'], 'DISCONNECTED')
        self.assertEqual(stream.bind.call_count, 2)
        self.assertNotEqual(stream.bind.call_args_list[0].args[0], stream.bind.call_args_list[1].args[0])

    def test_transport_error_closes_reply_socket_without_claiming_channel(self):
        stream = mock.Mock()
        stream.connect.side_effect = PermissionError('fixture')
        with mock.patch.object(ap_channel.socket, 'AF_UNIX', 1, create=True), mock.patch.object(ap_channel.socket, 'socket', return_value=stream):
            with self.assertRaises(PermissionError):
                ap_channel.read_status()
        stream.close.assert_called_once()


if __name__ == '__main__':
    unittest.main()
