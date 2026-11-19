<?php
declare(strict_types=1);

use dokuwiki\Extension\SyntaxPlugin;
use dokuwiki\Parsing\ModeRegistry;

if (!defined('DOKU_INC')) die();

/**
 * Renders the submission form ({{petar}}) or jobs journal ({{petar>jobs}}).
 */
final class syntax_plugin_petar extends SyntaxPlugin
{
    private const ALLOWED_TOOLS = ['yt-dlp', 'fabric', 'echo-test'];

    /**
     * @return string
     */
    public function getType(): string
    {
        return ModeRegistry::CATEGORY_SUBSTITUTION; // = 'substition' — каноническое имя категории ядра
    }

    /**
     * @return string
     */
    public function getPType(): string
    {
        return 'block'; // Both modes emit block-level markup.
    }

    /**
     * @return int
     */
    public function getSort(): int
    {
        return 305;
    }

    /**
     * @param string $mode
     * @return void
     */
    public function connectTo($mode): void
    {
        $this->Lexer->addSpecialPattern('\{\{petar\}\}', $mode, 'plugin_petar');
        $this->Lexer->addSpecialPattern('\{\{petar>jobs\}\}', $mode, 'plugin_petar');
    }

    /**
     * @param string $match
     * @param int $state
     * @param int $pos
     * @param Doku_Handler $handler
     * @return array
     */
    public function handle($match, $state, $pos, Doku_Handler $handler): array
    {
        return ['mode' => $match === '{{petar>jobs}}' ? 'jobs' : 'form'];
    }

    /**
     * @param string $format
     * @param Doku_Renderer $renderer
     * @param array $data
     * @return bool
     */
    public function render($format, Doku_Renderer $renderer, $data): bool
    {
        if ($format !== 'xhtml') {
            return false;
        }

        // Disable page caching because output depends on current user ACL and session token
        $renderer->info['cache'] = false;

        // Check ACL: user must be logged in and belong to 'ai' or 'admin' group
        if (!$this->checkAcl()) {
            $errorMsg = hsc($this->getLang('err_forbidden'));
            $renderer->doc .= '<div class="error">' . $errorMsg . '</div>';
            return true;
        }

        static $widgetNumber = 0;
        $id = 'petar-' . ++$widgetNumber;
        if (($data['mode'] ?? 'form') === 'jobs') {
            $renderer->doc .= $this->renderJobs($id);
            return true;
        }

        $strings = [];
        foreach ([
            'label_message', 'label_url', 'label_text', 'hint_message', 'hint_url',
            'hint_text', 'status_checking', 'status_ready', 'status_busy',
            'status_accepted', 'err_backend', 'err_request', 'err_submit_rejected', 'err_submit_uncertain',
            'label_status', 'label_input_id', 'label_process_id', 'label_request_id',
        ] as $key) {
            $strings[$key] = $this->getLang($key);
        }
        $ui = hsc(json_encode($strings, JSON_THROW_ON_ERROR));
        $sectok = hsc(getSecurityToken());
        $labelTool = hsc($this->getLang('label_tool'));
        $labelMessage = hsc($this->getLang('label_url'));
        $btnSubmit = hsc($this->getLang('btn_submit'));

        $optionsHtml = '';
        foreach (self::ALLOWED_TOOLS as $tool) {
            $safeTool = hsc($tool);
            $optionsHtml .= '<option value="' . $safeTool . '">' . $safeTool . '</option>';
        }

        $renderer->doc .= '<div class="plugin_petar" data-ui="' . $ui . '">';
        $renderer->doc .= '<form id="' . $id . '-form" method="post" action="">';
        
        $renderer->doc .= '<p class="row">';
        $renderer->doc .= '<label for="' . $id . '-tool">' . $labelTool . '</label>';
        $renderer->doc .= '<select id="' . $id . '-tool" name="tool">' . $optionsHtml . '</select>';
        $renderer->doc .= '</p>';

        $renderer->doc .= '<p class="row">';
        $renderer->doc .= '<label class="petar-message-label" for="' . $id . '-message">' . $labelMessage . '</label>';
        $renderer->doc .= '<textarea id="' . $id . '-message" name="message" rows="6" required'
            . ' aria-describedby="' . $id . '-hint" placeholder="' . hsc($this->getLang('hint_url')) . '"></textarea>';
        $renderer->doc .= '<small class="petar-hint" id="' . $id . '-hint">' . hsc($this->getLang('hint_url')) . '</small>';
        $renderer->doc .= '</p>';
        $renderer->doc .= '<p class="row petar-pattern" hidden>';
        $renderer->doc .= '<label for="' . $id . '-pattern">' . hsc($this->getLang('label_pattern')) . '</label>';
        $renderer->doc .= '<input id="' . $id . '-pattern" name="pattern" type="text" value="summarize"'
            . ' pattern="[A-Za-z0-9_\-]{1,128}" maxlength="128" disabled />';
        $renderer->doc .= '</p>';

        $renderer->doc .= '<input type="hidden" name="sectok" value="' . $sectok . '" />';
        
        $renderer->doc .= '<p class="row">';
        $renderer->doc .= '<button type="submit" class="button" disabled>' . $btnSubmit . '</button>';
        $renderer->doc .= '<button type="button" class="button petar-retry">' . hsc($this->getLang('btn_retry')) . '</button>';
        $renderer->doc .= '</p>';

        $renderer->doc .= '</form>';
        $renderer->doc .= '<p class="petar-status" role="status" aria-live="polite">' . hsc($this->getLang('status_checking')) . '</p>';
        $renderer->doc .= '<div id="' . $id . '-result" class="petar-result" role="status" aria-live="polite" hidden></div>';
        $renderer->doc .= '</div>';

        return true;
    }

    private function renderJobs(string $id): string
    {
        $fields = [
            'job_id' => 'jobs_job_id',
            'process_id' => 'label_process_id',
            'tool' => 'label_tool',
            'status' => 'label_status',
            'created_at' => 'jobs_created_at',
            'updated_at' => 'jobs_updated_at',
        ];
        $strings = [];
        foreach (['loading', 'empty', 'error', 'pending', 'dispatched', 'in_progress', 'completed', 'failed',
            'result_open', 'result_loading', 'result_not_ready', 'result_failed', 'result_missing',
            'result_unavailable', 'result_error'] as $key) {
            $strings[$key] = $this->getLang('jobs_' . $key);
        }
        $ui = hsc(json_encode($strings, JSON_THROW_ON_ERROR));
        $html = '<section id="' . $id . '-jobs" data-petar-jobs="" data-ui="' . $ui . '" aria-labelledby="' . $id . '-title">';
        $html .= '<h3 id="' . $id . '-title">' . hsc($this->getLang('jobs_title')) . '</h3>';
        $html .= '<div class="petar-jobs-controls"><label for="' . $id . '-filter">' . hsc($this->getLang('label_status')) . '</label>';
        $html .= '<select id="' . $id . '-filter"><option value="">' . hsc($this->getLang('jobs_all')) . '</option>';
        foreach (['pending', 'dispatched', 'in_progress', 'completed', 'failed'] as $status) {
            $html .= '<option value="' . $status . '">' . hsc($strings[$status]) . '</option>';
        }
        $html .= '</select><button type="button" class="button petar-jobs-refresh">' . hsc($this->getLang('jobs_refresh')) . '</button></div>';
        $html .= '<p class="petar-jobs-status" role="status"></p><div class="petar-jobs-table" hidden>';
        $html .= '<table><thead><tr>';
        foreach ($fields as $key) {
            $html .= '<th scope="col">' . hsc($this->getLang($key)) . '</th>';
        }
        $html .= '<th scope="col">' . hsc($this->getLang('jobs_result_title')) . '</th></tr></thead><tbody></tbody></table></div>';
        $html .= '<button type="button" class="button petar-jobs-more" hidden>' . hsc($this->getLang('jobs_more')) . '</button>';
        $html .= '<article id="' . $id . '-card" class="petar-job-card" hidden aria-labelledby="' . $id . '-card-title">';
        $html .= '<h4 id="' . $id . '-card-title">' . hsc($this->getLang('jobs_result_title')) . '</h4>';
        $html .= '<button type="button" class="button petar-job-close">' . hsc($this->getLang('jobs_result_close')) . '</button><dl>';
        foreach ($fields as $field => $key) {
            $html .= '<dt>' . hsc($this->getLang($key)) . '</dt><dd data-job-field="' . $field . '"></dd>';
        }
        $html .= '</dl><p class="petar-job-status" role="status"></p><pre class="petar-job-public" hidden></pre></article></section>';
        return $html;
    }

    /**
     * Check ACL: user must be logged in and belong to 'ai' or 'admin' group
     *
     * @return bool
     */
    private function checkAcl(): bool
    {
        global $INPUT, $USERINFO;

        $user = $INPUT->server->str('REMOTE_USER');
        if ($user === '') {
            return false;
        }

        $groups = (array)($USERINFO['grps'] ?? []);

        return in_array('ai', $groups, true) || in_array('admin', $groups, true);
    }
}
