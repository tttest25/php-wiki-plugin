<?php
if (!defined('DOKU_INC')) die();

use dokuwiki\Extension\ActionPlugin;
use dokuwiki\Extension\Event;
use dokuwiki\plugin\petar\PetarClient;
use dokuwiki\Logger;

/**
 * BFF Proxy Controller for Petar plugin
 *
 * Handles AJAX requests: plugin_petar
 * Routes: action=readyz|submit|status
 * Security: CSRF (for POST) + ACL (@ai/@admin)
 */
final class action_plugin_petar extends ActionPlugin
{
    /**
     * Register AJAX hook
     *
     * @param Doku_Event_Handler $controller
     */
    public function register(Doku_Event_Handler $controller): void
    {
        $controller->register_hook('AJAX_CALL_UNKNOWN', 'BEFORE', $this, 'handleAjax');
    }

    /**
     * Handle AJAX call for plugin_petar
     *
     * @param Event $event
     */
    public function handleAjax(Event $event): void
    {
        if ($event->data !== 'plugin_petar') {
            return;
        }

        // Stop DokuWiki further processing
        $event->stopPropagation();
        $event->result = '';

        $action = (string)($_REQUEST['action'] ?? 'readyz');
        $method = $_SERVER['REQUEST_METHOD'] ?? 'GET';

        // Security checks: ACL & CSRF
        if (!$this->checkAcl()) {
            return;
        }

        if ($method === 'POST' && !$this->checkCsrf()) {
            return;
        }

        $requestId = $this->getRequestId();

        try {
            $client = $this->createClient();

            switch ($action) {
                case 'readyz':
                    $result = $client->checkReadiness();
                    $this->sendJson($result, $result['http_status'] ?? 200, $result['request_id'] ?? $requestId);
                    break;

                case 'submit':
                    if ($method !== 'POST') {
                        $this->sendJson(['success' => false, 'error' => 'Method not allowed for submit'], 405, $requestId);
                        return;
                    }

                    $tool = (string)($_POST['tool'] ?? '');
                    $message = (string)($_POST['message'] ?? '');
                    $user = (string)($_SERVER['REMOTE_USER'] ?? '');

                    $result = $client->submitInput($tool, $message, $user, $requestId);
                    $this->sendJson($result, $result['http_status'] ?? 200, $result['request_id'] ?? $requestId);
                    break;

                case 'status':
                    $id = (string)($_REQUEST['id'] ?? '');
                    $result = $client->getStatus($id);
                    $this->sendJson($result, 200, $requestId);
                    break;

                default:
                    $this->sendJson(['success' => false, 'error' => 'Unknown action: ' . $action], 404, $requestId);
                    break;
            }
        } catch (InvalidArgumentException $e) {
            Logger::error('petar', sprintf('Invalid argument [req_id=%s]: %s', $requestId, $e->getMessage()), __FILE__, __LINE__);
            $this->sendJson(['success' => false, 'error' => $e->getMessage()], 400, $requestId);
        } catch (RuntimeException $e) {
            Logger::error('petar', sprintf('Runtime error [req_id=%s]: %s', $requestId, $e->getMessage()), __FILE__, __LINE__);
            $code = $e->getCode() >= 400 && $e->getCode() < 600 ? $e->getCode() : 502;
            $this->sendJson(['success' => false, 'error' => $e->getMessage()], $code, $requestId);
        } catch (Exception $e) {
            Logger::error('petar', sprintf('Unexpected error [req_id=%s]: %s', $requestId, $e->getMessage()), __FILE__, __LINE__);
            $this->sendJson(['success' => false, 'error' => 'Internal server error'], 500, $requestId);
        }
    }

    /**
     * Check ACL: user must be logged in and belong to 'ai' or 'admin' group
     *
     * @return bool
     */
    private function checkAcl(): bool
    {
        global $USERINFO;

        $user = $_SERVER['REMOTE_USER'] ?? '';
        if ($user === '') {
            Logger::error('petar', 'Access denied: unauthenticated request', __FILE__, __LINE__);
            $this->sendJson(['success' => false, 'error' => 'Access denied: authentication required'], 403);
            return false;
        }

        $groups = $USERINFO['grps'] ?? [];
        if (!is_array($groups)) {
            $groups = [];
        }

        // Check if user is in 'ai' or 'admin' group
        if (!in_array('ai', $groups, true) && !in_array('admin', $groups, true)) {
            Logger::error('petar', sprintf('Access denied for user "%s": missing "ai" or "admin" group', $user), __FILE__, __LINE__);
            $this->sendJson(['success' => false, 'error' => 'Access denied: insufficient privileges'], 403);
            return false;
        }

        return true;
    }

    /**
     * Check CSRF token for POST requests
     *
     * @return bool
     */
    private function checkCsrf(): bool
    {
        if (!checkSecurityToken()) {
            Logger::error('petar', 'CSRF validation failed (sectok mismatch)', __FILE__, __LINE__);
            $this->sendJson(['success' => false, 'error' => 'Security token mismatch (CSRF)'], 403);
            return false;
        }
        return true;
    }

    /**
     * Get or generate request ID
     *
     * @return string
     */
    private function getRequestId(): string
    {
        $requestId = $_SERVER['HTTP_X_REQUEST_ID'] ?? $_REQUEST['sectok'] ?? '';
        $requestId = trim($requestId);
        
        if (preg_match('/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i', $requestId)) {
            return $requestId;
        }

        // Generate UUIDv4
        $data = random_bytes(16);
        $data[6] = chr(ord($data[6]) & 0x0f | 0x40);
        $data[8] = chr(ord($data[8]) & 0x3f | 0x80);
        return vsprintf('%s%s-%s-%s-%s-%s%s%s', str_split(bin2hex($data), 4));
    }

    /**
     * Create PetarClient instance using plugin configuration
     *
     * @return PetarClient
     */
    private function createClient(): PetarClient
    {
        $url = (string)$this->getConf('petar_url');
        $secret = (string)$this->getConf('api_secret');
        $timeout = (int)$this->getConf('timeout');

        if ($url === '') {
            $url = 'http://petar_app:8080';
        }
        if ($timeout <= 0) {
            $timeout = 15;
        }

        return new PetarClient($url, $secret, $timeout);
    }

    /**
     * Send JSON response and exit
     *
     * @param array $data
     * @param int $httpCode
     * @param string $requestId
     */
    private function sendJson(array $data, int $httpCode = 200, string $requestId = ''): void
    {
        http_response_code($httpCode);
        header('Content-Type: application/json; charset=utf-8');
        if ($requestId !== '') {
            header('X-Request-ID: ' . $requestId);
        }
        echo json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
        exit;
    }
}
