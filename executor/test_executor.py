"""Unit tests for executor mode selection and request auth.

Run with: python3 -m unittest executor.test_executor (from repo root)
or: python3 -m unittest test_executor (from executor/).
"""
import unittest

import executor


class TestLLMMode(unittest.TestCase):
    """The executor must enter LLM mode for keyless endpoints (Ollama, vLLM,
    gateways) — MODEL_ENDPOINT alone is sufficient, no API key required."""

    def setUp(self):
        self._saved = (executor.MODEL_API_KEY, executor.USE_VERTEX, executor.MODEL_ENDPOINT)

    def tearDown(self):
        executor.MODEL_API_KEY, executor.USE_VERTEX, executor.MODEL_ENDPOINT = self._saved

    def _set(self, api_key='', use_vertex=False, endpoint=''):
        executor.MODEL_API_KEY = api_key
        executor.USE_VERTEX = use_vertex
        executor.MODEL_ENDPOINT = endpoint

    def test_api_key_enables_llm_mode(self):
        self._set(api_key='sk-test')
        self.assertTrue(executor.llm_mode())

    def test_vertex_enables_llm_mode(self):
        self._set(use_vertex=True)
        self.assertTrue(executor.llm_mode())

    def test_keyless_endpoint_enables_llm_mode(self):
        self._set(endpoint='http://ollama.ai-agents:11434/v1')
        self.assertTrue(executor.llm_mode())

    def test_nothing_configured_is_demo_mode(self):
        self._set()
        self.assertFalse(executor.llm_mode())


class TestOpenAIHeaders(unittest.TestCase):
    """No Authorization header should be sent when there is no API key —
    a malformed 'Bearer ' (empty) header makes strict gateways return 401,
    which the executor treats as a silent fall-back to demo mode."""

    def setUp(self):
        self._saved = executor.MODEL_API_KEY

    def tearDown(self):
        executor.MODEL_API_KEY = self._saved

    def test_header_present_with_key(self):
        executor.MODEL_API_KEY = 'sk-test'
        headers = executor.openai_headers()
        self.assertEqual(headers.get('Authorization'), 'Bearer sk-test')

    def test_no_auth_header_without_key(self):
        executor.MODEL_API_KEY = ''
        headers = executor.openai_headers()
        self.assertNotIn('Authorization', headers)
        self.assertEqual(headers.get('Content-Type'), 'application/json')


class TestOutputExitCode(unittest.TestCase):
    """A step whose output is an error must exit non-zero so the Job fails
    and the controller records a Failed step — not a silent success."""

    def test_error_output_exits_nonzero(self):
        self.assertEqual(executor.output_exit_code({'error': 'Read timed out', 'step': 'write'}), 1)

    def test_successful_output_exits_zero(self):
        self.assertEqual(executor.output_exit_code({'response': 'a haiku', '_metrics': {}}), 0)

    def test_demo_output_exits_zero(self):
        self.assertEqual(executor.output_exit_code({'status': 'completed', 'mode': 'demo'}), 0)

    def test_non_dict_output_exits_zero(self):
        self.assertEqual(executor.output_exit_code('raw text'), 0)


class TestParseResponses(unittest.TestCase):
    """Model content that parses as non-object JSON (a bare quoted string,
    number, or list) must be wrapped in {'response': ...} — downstream code
    (extract_structured_fields, history, variable substitution) requires a
    dict and crashes on str."""

    def _openai(self, content):
        return {'choices': [{'message': {'role': 'assistant', 'content': content}}]}

    def test_openai_json_object_passthrough(self):
        out, reqs = executor.parse_openai_response(self._openai('{"verdict": "approve"}'), [])
        self.assertEqual(out, {'verdict': 'approve'})
        self.assertEqual(reqs, [])

    def test_openai_bare_json_string_is_wrapped(self):
        out, _ = executor.parse_openai_response(self._openai('"a quoted haiku"'), [])
        self.assertIsInstance(out, dict)
        self.assertEqual(out['response'], '"a quoted haiku"')

    def test_openai_plain_text_is_wrapped(self):
        out, _ = executor.parse_openai_response(self._openai('plain haiku text'), [])
        self.assertEqual(out['response'], 'plain haiku text')

    def test_anthropic_bare_json_list_is_wrapped(self):
        response = {'content': [{'type': 'text', 'text': '[1, 2, 3]'}], 'usage': {}}
        out, _ = executor.parse_anthropic_response(response, [])
        self.assertIsInstance(out, dict)
        self.assertEqual(out['response'], '[1, 2, 3]')


class TestOpenAIUsage(unittest.TestCase):
    """OpenAI-format responses report usage as prompt_tokens/completion_tokens
    — these must feed cost tracking like the anthropic path does."""

    def test_extracts_usage(self):
        result = {'usage': {'prompt_tokens': 120, 'completion_tokens': 45}}
        self.assertEqual(executor.openai_usage(result), (120, 45))

    def test_missing_usage_is_zero(self):
        self.assertEqual(executor.openai_usage({}), (0, 0))
        self.assertEqual(executor.openai_usage({'usage': {}}), (0, 0))


class TestModelTunables(unittest.TestCase):
    """max_tokens and the HTTP timeout must be env-tunable — hardcoded 4096
    tokens makes small local models generate past the read timeout."""

    def test_defaults(self):
        self.assertEqual(executor.env_int('PURKO_TEST_UNSET_VAR', 4096), 4096)

    def test_env_override(self):
        import os
        os.environ['PURKO_TEST_SET_VAR'] = '256'
        try:
            self.assertEqual(executor.env_int('PURKO_TEST_SET_VAR', 4096), 256)
        finally:
            del os.environ['PURKO_TEST_SET_VAR']

    def test_invalid_value_falls_back(self):
        import os
        os.environ['PURKO_TEST_BAD_VAR'] = 'not-a-number'
        try:
            self.assertEqual(executor.env_int('PURKO_TEST_BAD_VAR', 120), 120)
        finally:
            del os.environ['PURKO_TEST_BAD_VAR']


if __name__ == '__main__':
    unittest.main()
