import json
import unittest

from week11_runtime import Week11Lab, validate_fixture


class FixtureSafetyTest(unittest.TestCase):
    def test_only_own_running_project_and_matching_service_can_be_stopped(self):
        lab = Week11Lab()
        calls = []
        # External Docker inspection is the boundary; exercise guard and command ordering.
        lab.created = True
        def run(command, *args, **kwargs):
            calls.append(command)
            if command[-3:] == ['ps', '-aq', 'redis']:
                return 'a' * 64 + '\n'
            if command[:2] == ['docker', 'inspect']:
                return json.dumps({'com.docker.compose.project': 'daily-project', 'com.docker.compose.service': 'redis'})
            self.fail('mutating command issued without matching ownership')
        lab.run = run
        with self.assertRaisesRegex(RuntimeError, 'ownership'):
            lab.service('redis', 'stop')
        self.assertEqual(len(calls), 2)

    def test_fixture_identity_rejects_daily_database_and_external_hosts(self):
        lab = Week11Lab()
        validate_fixture(lab)
        for key, value in [('DB_NAME', 'nexusdesk'), ('DB_HOST', '10.0.0.1'), ('CHAT_PROVIDER', 'deepseek')]:
            old = lab.env[key]
            lab.env[key] = value
            with self.assertRaises(ValueError):
                validate_fixture(lab)
            lab.env[key] = old
        lab.env['DEEPSEEK_API_KEY'] = 'must-never-send'
        with self.assertRaises(ValueError):
            validate_fixture(lab)

    def test_stopped_or_unrecognized_targets_cannot_be_mutated(self):
        lab = Week11Lab()
        for target, action in [('redis', 'stop'), ('db', 'start'), ('other', 'stop'), ('redis', 'down')]:
            with self.assertRaises(ValueError):
                lab.service(target, action)


if __name__ == '__main__':
    unittest.main()
