local c = ...
local group = vim.api.nvim_create_augroup('NotesflowAppearance', { clear = true })

local function theme()
  local colors = {
    Normal = { fg = c.text, bg = c.background },
    NormalNC = { link = 'Normal' },
    EndOfBuffer = { fg = c.background, bg = c.background },
    SignColumn = { fg = c.muted, bg = c.background },
    FoldColumn = { fg = c.muted, bg = c.background },
    CursorLine = { bg = c.surface },
    CursorLineNr = { fg = c.cyan, bg = c.surface, bold = true },
    LineNr = { fg = c.line, bg = c.background },
    WinSeparator = { fg = c.line, bg = c.background },
    StatusLine = { fg = c.muted, bg = c.surface },
    NormalFloat = { fg = c.text, bg = c.surface },
    FloatBorder = { fg = c.line, bg = c.surface },
    Pmenu = { fg = c.text, bg = c.surface },
    PmenuSel = { fg = c.background, bg = c.cyan },
    Visual = { bg = c.selection },
    Comment = { fg = c.muted, italic = true },
    Statement = { fg = c.cyan },
    Type = { fg = c.purple },
    String = { fg = c.yellow },
    Constant = { fg = c.purple },
    Special = { fg = c.purple },
    Identifier = { fg = c.text },
    Title = { fg = c.cyan, bold = true },
    Todo = { fg = c.pink, bold = true },
    texStatement = { link = 'Statement' },
    texDelimiter = { link = 'Special' },
    texSection = { link = 'Title' },
    texSectionMarker = { link = 'Title' },
    ['@keyword'] = { link = 'Statement' },
    ['@comment'] = { link = 'Comment' },
    ['@string'] = { link = 'String' },
    ['@markup.heading'] = { link = 'Title' },
  }
  for name, value in pairs(colors) do
    vim.api.nvim_set_hl(0, name, value)
  end
end

local function chrome()
  for key, value in pairs({laststatus = 0, showtabline = 0, showmode = false, ruler = false}) do
    if vim.o[key] ~= value then vim.o[key] = value end
  end
  if vim.wo.winbar ~= '' then vim.wo.winbar = '' end
end

theme()
chrome()
vim.api.nvim_create_autocmd('ColorScheme', { group = group, callback = theme })
vim.api.nvim_create_autocmd({'BufWinEnter', 'WinEnter'}, { group = group, callback = chrome })
