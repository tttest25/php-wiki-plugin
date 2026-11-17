<?php
declare(strict_types=1);

use dokuwiki\Extension\SyntaxPlugin;
use dokuwiki\Parsing\ModeRegistry;

if (!defined('DOKU_INC')) die();

/**
 * DokuWiki Syntax Plugin petar: renders the petar submission form for {{petar}}
 */
final class syntax_plugin_petar extends SyntaxPlugin
{
    private const ALLOWED_TOOLS = ['yt-dlp', 'fabric', 'echo-test'];

    /**
     * @return string
     */
    public function getType(): string
    {
        return ModeRegistry::CATEGORY_SUBSTITUTION;
    }

    /**
     * @return string
     */
    public function getPType(): string
    {
        return 'normal';
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
        return [];
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

        $sectok = hsc(getSecurityToken());
        $labelTool = hsc($this->getLang('label_tool'));
        $labelMessage = hsc($this->getLang('label_message'));
        $btnSubmit = hsc($this->getLang('btn_submit'));

        $optionsHtml = '';
        foreach (self::ALLOWED_TOOLS as $tool) {
            $safeTool = hsc($tool);
            $optionsHtml .= '<option value="' . $safeTool . '">' . $safeTool . '</option>';
        }

        $renderer->doc .= '<div class="plugin_petar">';
        $renderer->doc .= '<form id="petar-form" method="post" action="">';
        
        $renderer->doc .= '<p class="row">';
        $renderer->doc .= '<label for="petar-tool">' . $labelTool . '</label>';
        $renderer->doc .= '<select id="petar-tool" name="tool">' . $optionsHtml . '</select>';
        $renderer->doc .= '</p>';

        $renderer->doc .= '<p class="row">';
        $renderer->doc .= '<label for="petar-message">' . $labelMessage . '</label>';
        $renderer->doc .= '<textarea id="petar-message" name="message" rows="6" required></textarea>';
        $renderer->doc .= '</p>';

        $renderer->doc .= '<input type="hidden" name="sectok" value="' . $sectok . '" />';
        
        $renderer->doc .= '<p class="row">';
        $renderer->doc .= '<button type="submit" class="button">' . $btnSubmit . '</button>';
        $renderer->doc .= '</p>';

        $renderer->doc .= '</form>';
        $renderer->doc .= '<div id="petar-result" class="petar-result" hidden></div>';
        $renderer->doc .= '</div>';

        return true;
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
