<?php
namespace dokuwiki\plugin\petar;

if (!defined('DOKU_INC')) die();

use dokuwiki\HTTP\DokuHTTPClient;
use dokuwiki\Logger;
use InvalidArgumentException;
use RuntimeException;

/**
 * Petar BFF API Client
 *
 * Encapsulates network calls to the Petar backend service using DokuHTTPClient,
 * enforcing security headers and normalizing backend responses.
 */
final class PetarClient
{
    private const MAX_ACTION_VERSION = 2_147_483_647; // Backend and PostgreSQL use int32.
    private const JOB_STATUSES = ['pending', 'dispatched', 'in_progress', 'completed', 'failed'];
    private const JOB_PAGE_SIZE = 20;
    private const MAX_JOB_RESULT_BYTES = 1_048_576;

    private string $baseUrl;
    private string $apiSecret;
    private int $timeout;

    /**
     * Constructor
     *
     * @param string $baseUrl
     * @param string $apiSecret
     * @param int $timeout
     * @throws InvalidArgumentException
     */
    public function __construct(string $baseUrl, string $apiSecret = '', int $timeout = 15)
    {
        $baseUrl = trim($baseUrl);
        if ($baseUrl === '') {
            throw new InvalidArgumentException('Petar base URL cannot be empty');
        }

        $parsed = parse_url($baseUrl);
        if ($parsed === false || empty($parsed['host']) || empty($parsed['scheme']) || !in_array($parsed['scheme'], ['http', 'https'], true)) {
            throw new InvalidArgumentException('Invalid Petar base URL scheme');
        }

        $this->baseUrl = rtrim($baseUrl, '/');
        $this->apiSecret = trim($apiSecret);
        
        if ($timeout <= 0) {
            throw new InvalidArgumentException('Timeout must be a positive integer');
        }
        $this->timeout = $timeout;
    }

    /**
     * Check backend readiness via GET /readyz
     *
     * Does not throw on network/HTTP errors; returns success => false.
     *
     * @return array Normalized envelope
     */
    public function checkReadiness(?string $requestId = null): array
    {
        $requestId = $requestId ?? $this->generateUuidV4();
        if (!$this->isValidUuidV4($requestId)) {
            throw new InvalidArgumentException('Invalid request ID format (UUIDv4 expected)');
        }
        $url = $this->baseUrl . '/readyz';

        $client = $this->newDirectClient();
        $client->headers['Accept'] = 'application/json';
        $client->headers['X-Request-ID'] = $requestId;

        $success = $client->sendRequest($url, '', 'GET');
        $status = (int)$client->status;
        $body = $client->resp_body;

        if (!$success || $status !== 200) {
            $errorMsg = 'Backend unavailable';
            Logger::error('petar', sprintf('Readiness check failed [req_id=%s, status=%d]', $requestId, $status), __FILE__, __LINE__);
            return [
                'success' => false,
                'http_status' => $status,
                'request_id' => $requestId,
                'data' => null,
                'error' => $errorMsg,
            ];
        }

        $data = json_decode($body, true);
        if (!is_array($data) || ($data['status'] ?? null) !== 'ready') {
            $errorMsg = 'Invalid JSON response from backend readiness check';
            Logger::error('petar', sprintf('Readiness parse failed [req_id=%s]: %s', $requestId, $errorMsg), __FILE__, __LINE__);
            return [
                'success' => false,
                'http_status' => $status,
                'request_id' => $requestId,
                'data' => null,
                'error' => $errorMsg,
            ];
        }

        return [
            'success' => true,
            'http_status' => $status,
            'request_id' => $requestId,
            'data' => ['status' => 'ready'],
            'error' => null,
        ];
    }

    /** Submit an action with the original JSON value via POST /api/v1/actions. */
    public function submitAction(string $action, int $version, string $dataJson, string $user, ?string $requestId = null): array
    {
        if (trim($action) === '' || !preg_match('//u', $action) || $version < 1 ||
            $version > self::MAX_ACTION_VERSION || !json_validate($dataJson)) {
            throw new InvalidArgumentException('Invalid action submission');
        }

        $user = trim(str_replace(["\r", "\n"], '', $user));
        if ($user === '') {
            throw new InvalidArgumentException('Wiki user cannot be empty');
        }

        if ($requestId !== null) {
            $requestId = trim($requestId);
            if (!$this->isValidUuidV4($requestId)) {
                throw new InvalidArgumentException('Invalid request ID format (UUIDv4 expected)');
            }
        } else {
            $requestId = $this->generateUuidV4();
        }

        // Embed the checked original value without a decode/encode round trip.
        $jsonBody = '{"action":' . json_encode($action, JSON_THROW_ON_ERROR)
            . ',"version":' . $version . ',"data":' . $dataJson . '}';
        $url = $this->baseUrl . '/api/v1/actions';

        $client = $this->newDirectClient();
        $client->debug = false;
        $client->headers['Accept'] = 'application/json';
        $client->headers['Content-Type'] = 'application/json';
        $client->headers['X-Request-ID'] = $requestId;
        $client->headers['X-Wiki-User'] = $user;

        if ($this->apiSecret !== '') {
            $client->headers['X-API-Key'] = $this->apiSecret;
        }

        // 3. Execute request
        $success = $client->sendRequest($url, $jsonBody, 'POST');
        $status = (int)$client->status;

        // A direct HTTP 400 is a rejection even if sendRequest reports false.
        if ($status === 400) {
            throw new InvalidArgumentException('Input rejected', 400);
        }

        if (!$success || $status !== 201) {
            Logger::error('petar', sprintf('Submit input failed [req_id=%s, status=%d]', $requestId, $status), __FILE__, __LINE__);
            throw new RuntimeException('Backend request failed', 502);
        }

        $data = json_decode($client->resp_body, true);
        if (!is_array($data)) {
            $errorMsg = 'Invalid JSON response from backend submit input';
            Logger::error('petar', sprintf('Submit parse failed [req_id=%s]: %s', $requestId, $errorMsg), __FILE__, __LINE__);
            throw new RuntimeException(sprintf('Invalid backend response [req_id=%s]', $requestId), 502);
        }

        // Validate required backend response fields
        if (!$this->isValidUuid($data['input_id'] ?? null) ||
            !$this->isValidUuid($data['process_id'] ?? null) || ($data['status'] ?? null) !== 'new') {
            $errorMsg = 'Missing required fields in backend submit response';
            Logger::error('petar', sprintf('Submit response structure error [req_id=%s]', $requestId), __FILE__, __LINE__);
            throw new RuntimeException(sprintf('Incomplete backend response structure [req_id=%s]', $requestId), 502);
        }

        return [
            'success' => true,
            'http_status' => $status,
            'request_id' => $requestId,
            'data' => ['input_id' => $data['input_id'], 'process_id' => $data['process_id'], 'status' => 'new'],
            'error' => null,
        ];
    }

    /** Read active action/version pairs in backend order. */
    public function listActions(string $user, string $requestId): array
    {
        $client = $this->newDirectClient();
        $client->debug = false;
        $client->headers['Accept'] = 'application/json';
        $client->headers['X-Request-ID'] = $requestId;
        $client->headers['X-Wiki-User'] = str_replace(["\r", "\n"], '', $user);
        if ($this->apiSecret !== '') {
            $client->headers['X-API-Key'] = $this->apiSecret;
        }
        $rows = $client->sendRequest($this->baseUrl . '/api/v1/actions', '', 'GET')
            && (int)$client->status === 200 ? json_decode($client->resp_body) : null;
        if (is_array($rows)) {
            $actions = [];
            foreach ($rows as $row) {
                if (!$row instanceof \stdClass || !is_string($row->action ?? null) || trim($row->action) === '' ||
                    !is_int($row->version ?? null) || $row->version < 1 ||
                    $row->version > self::MAX_ACTION_VERSION || !is_string($row->title ?? null)) {
                    break;
                }
                $actions[] = ['action' => $row->action, 'version' => $row->version, 'title' => $row->title];
            }
            if (count($actions) === count($rows)) {
                return $actions;
            }
        }
        Logger::error('petar', 'Action catalog unavailable [req_id=' . $requestId . ']', __FILE__, __LINE__);
        throw new RuntimeException('Backend request failed', 502);
    }

    /** Read a shared page; the wiki user is for tracing, not filtering. */
    public function listJobs(string $user, ?string $status = null, ?string $cursor = null, ?string $requestId = null): array
    {
        $query = ['limit' => self::JOB_PAGE_SIZE + 1];
        if ($status !== null) {
            if (!in_array($status, self::JOB_STATUSES, true)) {
                throw new InvalidArgumentException('Invalid field: status');
            }
            $query['status'] = $status;
        }
        if ($cursor !== null) {
            if (!$this->isValidUuid($cursor)) {
                throw new InvalidArgumentException('Invalid field: cursor');
            }
            $query['cursor'] = $cursor;
        }
        $requestId ??= $this->generateUuidV4();
        if (!$this->isValidUuidV4($requestId)) {
            throw new InvalidArgumentException('Invalid request ID format (UUIDv4 expected)');
        }

        $client = $this->newDirectClient();
        $client->debug = false;
        $client->headers['Accept'] = 'application/json';
        $client->headers['X-Request-ID'] = $requestId;
        $client->headers['X-Wiki-User'] = str_replace(["\r", "\n"], '', $user);
        if ($this->apiSecret !== '') {
            $client->headers['X-API-Key'] = $this->apiSecret;
        }
        $url = $this->baseUrl . '/api/v1/jobs?' . http_build_query($query, '', '&', PHP_QUERY_RFC3986);
        if (!$client->sendRequest($url, '', 'GET') || (int)$client->status !== 200) {
            $this->failJobs($requestId);
        }

        // Preserve JSON object/array types, including for empty containers.
        $rows = json_decode($client->resp_body);
        if (!is_array($rows) || count($rows) > self::JOB_PAGE_SIZE + 1) {
            $this->failJobs($requestId);
        }
        $jobs = [];
        foreach ($rows as $row) {
            if (!$row instanceof \stdClass ||
                !$this->isValidUuid($row->id ?? null) || !$this->isValidUuid($row->process_id ?? null) ||
                !is_string($row->tool_name ?? null) || trim($row->tool_name) === '' ||
                !in_array($row->status ?? null, self::JOB_STATUSES, true) ||
                !$this->isJobDate($row->created_at ?? null) || !$this->isJobDate($row->updated_at ?? null)) {
                $this->failJobs($requestId);
            }
            $jobs[] = [
                'job_id' => $row->id,
                'process_id' => $row->process_id,
                'tool' => $row->tool_name,
                'status' => $row->status,
                'created_at' => $row->created_at,
                'updated_at' => $row->updated_at,
            ];
        }

        $nextCursor = null;
        if (count($jobs) > self::JOB_PAGE_SIZE) {
            array_pop($jobs);
            $nextCursor = $jobs[self::JOB_PAGE_SIZE - 1]['job_id'];
        }
        return ['jobs' => $jobs, 'next_cursor' => $nextCursor];
    }

    /** Read the public result of one job, never of its process. */
    public function getJobResult(string $user, string $jobId, ?string $requestId = null): array
    {
        if (!$this->isValidUuid($jobId)) {
            throw new InvalidArgumentException('Invalid field: job_id');
        }
        $requestId ??= $this->generateUuidV4();
        if (!$this->isValidUuidV4($requestId)) {
            throw new InvalidArgumentException('Invalid request ID format (UUIDv4 expected)');
        }

        $client = $this->newDirectClient();
        $client->debug = false;
        $client->headers['Accept'] = 'application/json';
        $client->headers['X-Request-ID'] = $requestId;
        $client->headers['X-Wiki-User'] = str_replace(["\r", "\n"], '', $user);
        if ($this->apiSecret !== '') {
            $client->headers['X-API-Key'] = $this->apiSecret;
        }
        $url = $this->baseUrl . '/api/v1/jobs/' . $jobId . '/result';
        $client->headers['Accept-encoding'] = 'identity';
        $client->keep_alive = false;
        $client->max_bodysize = self::MAX_JOB_RESULT_BYTES;
        if (!$client->sendRequest($url, '', 'GET') || strlen($client->resp_body) > self::MAX_JOB_RESULT_BYTES) {
            $this->failJobResult($requestId);
        }
        $status = (int)$client->status;
        $body = json_decode($client->resp_body);
        if ($status === 200 && $body instanceof \stdClass && count(get_object_vars($body)) === 3 &&
            is_string($body->type ?? null) && $body->type !== '' && ($body->version ?? null) === 1 &&
            ($body->data ?? null) instanceof \stdClass && json_encode($body) !== false) {
            return ['http_status' => 200, 'data' => ['job_id' => $jobId, 'result' => $body]];
        }
        if ($status === 202 && in_array($body->status ?? null, ['pending', 'dispatched', 'in_progress'], true)) {
            return ['http_status' => 202, 'data' => ['status' => $body->status]];
        }
        $error = match ($status) {
            404 => 'job_not_found',
            409 => ($body->error ?? null) === 'job_failed' ? 'job_failed' : null,
            410 => ($body->error ?? null) === 'result_unavailable' ? 'result_unavailable' : null,
            default => null,
        };
        if ($error !== null) {
            return ['http_status' => $status, 'data' => ['error' => $error]];
        }
        $this->failJobResult($requestId);
    }

    private function failJobResult(string $requestId): never
    {
        Logger::error('petar', 'Job result request failed [req_id=' . $requestId . ']', __FILE__, __LINE__);
        throw new RuntimeException('Backend request failed', 502);
    }

    private function newDirectClient(): DokuHTTPClient
    {
        $client = new DokuHTTPClient();
        $client->timeout = $this->timeout;
        $client->max_redirect = 0;
        return $client;
    }

    private function isJobDate(mixed $value): bool
    {
        return is_string($value) &&
            preg_match('/\A(\d{4})-(\d{2})-(\d{2})T(?:[01]\d|2[0-3]):[0-5]\d:(?:[0-5]\d|60)(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)\z/i', $value, $parts) &&
            checkdate((int)$parts[2], (int)$parts[3], (int)$parts[1]);
    }

    private function failJobs(string $requestId): never
    {
        Logger::error('petar', 'Jobs request failed [req_id=' . $requestId . ']', __FILE__, __LINE__);
        throw new RuntimeException('Backend request failed', 502);
    }

    /**
     * Reserved method for polling task/job status
     *
     * @param string $id
     * @return array
     * @throws RuntimeException
     */
    public function getStatus(string $id): array
    {
        throw new RuntimeException('Not implemented');
    }

    /**
     * Generate a UUIDv4 string
     *
     * @return string
     */
    private function generateUuidV4(): string
    {
        $data = random_bytes(16);
        $data[6] = chr(ord($data[6]) & 0x0f | 0x40); // version 4
        $data[8] = chr(ord($data[8]) & 0x3f | 0x80); // variant rfc 4122
        return vsprintf('%s%s-%s-%s-%s-%s%s%s', str_split(bin2hex($data), 4));
    }

    /**
     * Validate UUIDv4 format
     *
     * @param string $uuid
     * @return bool
     */
    private function isValidUuidV4(string $uuid): bool
    {
        return (bool)preg_match('/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iD', $uuid);
    }

    private function isValidUuid($uuid): bool
    {
        return is_string($uuid) && (bool)preg_match('/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iD', $uuid);
    }
}
