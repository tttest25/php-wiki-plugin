<?php
if (!defined('DOKU_INC')) die();

use dokuwiki\Extension\ActionPlugin;
use dokuwiki\Extension\Event;
use dokuwiki\plugin\petar\PetarClient;
use dokuwiki\Logger;

/** DokuWiki-only BFF entry point. No backend status callback is exposed. */
final class action_plugin_petar extends ActionPlugin
{
    private string $requestId = '';

    public function register(Doku_Event_Handler $controller): void
    {
        $controller->register_hook('AJAX_CALL_UNKNOWN', 'BEFORE', $this, 'handleAjax');
    }

    public function handleAjax(Event $event): void
    {
        global $INPUT, $USERINFO;

        if ($event->data !== 'plugin_petar') {
            return;
        }
        $event->stopPropagation();
        $event->result = '';
        $this->requestId = $this->getRequestId();

        $user = $INPUT->server->str('REMOTE_USER');
        $groups = (array)($USERINFO['grps'] ?? []);
        if ($user === '' || (!in_array('ai', $groups, true) && !in_array('admin', $groups, true))) {
            $this->fail('Access denied', 403);
            return;
        }
        $method = $INPUT->server->str('REQUEST_METHOD', 'GET');
        if ($method === 'POST' && !checkSecurityToken()) {
            $this->fail('Security token mismatch (CSRF)', 403);
            return;
        }

        try {
            $action = $this->stringParam($INPUT, 'action', 'readyz');
            if (!in_array($action, ['readyz', 'submit', 'jobs', 'job_result'], true)) {
                $this->fail('Unknown action', 404);
                return;
            }
            $allowedMethod = $action === 'submit' ? 'POST' : 'GET';
            if ($method !== $allowedMethod) {
                header('Allow: ' . $allowedMethod);
                $this->fail('Method not allowed', 405);
                return;
            }

            // Configuration errors are not user-input errors.
            try {
                $client = new PetarClient(
                    (string)$this->getConf('petar_url'),
                    (string)$this->getConf('api_secret'),
                    (int)$this->getConf('timeout')
                );
            } catch (InvalidArgumentException $e) {
                $this->fail('Invalid backend configuration', 500);
                return;
            }

            if ($action === 'readyz') {
                $result = $client->checkReadiness($this->requestId);
                if (!$result['success']) {
                    $this->sendJson(['status' => 'unavailable', 'error' => 'Backend unavailable'], 503);
                    return;
                }
                $this->sendJson($result['data'], 200);
                return;
            }

            if ($action === 'jobs') {
                $status = $INPUT->get->has('status') ? $this->stringParam($INPUT->get, 'status') : null;
                $cursor = $INPUT->get->has('cursor') ? $this->stringParam($INPUT->get, 'cursor') : null;
                $page = $client->listJobs($user, $status, $cursor, $this->requestId);
                header('Cache-Control: no-store');
                $this->sendJson($page, 200);
                return;
            }

            if ($action === 'job_result') {
                header('Cache-Control: no-store');
                $jobId = $this->stringParam($INPUT->get, 'job_id');
                $result = $client->getJobResult($user, $jobId, $this->requestId);
                $this->sendJson($result['data'], $result['http_status']);
                return;
            }

            $tool = $this->stringParam($INPUT->post, 'tool');
            $message = $this->stringParam($INPUT->post, 'message');
            $pattern = $INPUT->post->has('pattern') ? $this->stringParam($INPUT->post, 'pattern') : null;
            $result = $client->submitInput($tool, $message, $user, $this->requestId, $pattern);
            $this->sendJson($result['data'], 201);
        } catch (InvalidArgumentException $e) {
            $this->fail($e->getMessage(), 400);
        } catch (RuntimeException $e) {
            $this->fail('Backend request failed', 502);
        } catch (Throwable $e) {
            $this->fail('Internal server error', 500);
        }
    }

    /** Reject arrays rather than converting them to "Array". */
    private function stringParam($input, string $name, string $default = ''): string
    {
        $value = $input->param($name, $default);
        if (!is_string($value)) {
            throw new InvalidArgumentException('Invalid field: ' . $name);
        }
        return $value;
    }

    private function getRequestId(): string
    {
        global $INPUT;
        $value = $INPUT->server->param('HTTP_X_REQUEST_ID', '');
        if (is_string($value) && preg_match('/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iD', $value)) {
            return $value;
        }
        $data = random_bytes(16);
        $data[6] = chr(ord($data[6]) & 0x0f | 0x40);
        $data[8] = chr(ord($data[8]) & 0x3f | 0x80);
        return vsprintf('%s%s-%s-%s-%s-%s%s%s', str_split(bin2hex($data), 4));
    }

    private function fail(string $message, int $code): void
    {
        Logger::error('petar', sprintf('%s [req_id=%s, status=%d]', $message, $this->requestId, $code), __FILE__, __LINE__);
        $this->sendJson(['success' => false, 'error' => $message], $code);
    }

    private function sendJson(array $data, int $httpCode): void
    {
        http_response_code($httpCode);
        header('Content-Type: application/json; charset=utf-8');
        header('X-Request-ID: ' . $this->requestId);
        $data['request_id'] = $this->requestId;
        echo json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE);
        exit;
    }
}
