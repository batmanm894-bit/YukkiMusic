/*
 * ● YukkiMusic
 * ○ A high-performance engine for streaming music in Telegram voicechats.
 *
 * Copyright (C) 2026 TheTeamVivek
 *
 * This program is free software: you can redistribute it and/or modify it under the
 * terms of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 *
 * Repository: https://github.com/TheTeamVivek/YukkiMusic
 */

package modules

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	tg "github.com/amarnathcjd/gogram/telegram"
	"resty.dev/v3"

	"main/internal/utils"
)

func init() {
	helpTexts["/calc"] = `<i>Quick arithmetic calculator — no need to leave Telegram.</i>

<u>Usage:</u>
<b>/calc &lt;expression&gt;</b>

<b>Supported:</b> <code>+  -  *  /  %  ^  ( )</code>

<b>Examples:</b>
<code>/calc 25*4+10</code>
<code>/calc (12+8)/4</code>
<code>/calc 2^10</code>`

	helpTexts["/font"] = `<i>Convert text into stylish unicode fonts.</i>

<u>Usage:</u>
<b>/font &lt;text&gt;</b> — Shows the text in every supported style.
<b>/font &lt;style&gt; &lt;text&gt;</b> — Shows the text in one specific style.

Use <b>/fonts</b> to see the list of available style names.

<b>Example:</b>
<code>/font bold Hello World</code>`

	helpTexts["/fonts"] = `<i>List all font styles supported by /font.</i>`

	helpTexts["/ton"] = `<i>Live TON (Toncoin) ⇄ USD price and converter.</i>

<u>Usage:</u>
<b>/ton</b> — Show the current TON price in USD.
<b>/ton &lt;amount&gt;</b> — Convert that many TON to USD.

See also <b>/usdton</b> to convert USD to TON.`

	helpTexts["/usdton"] = `<i>Convert USD to TON (Toncoin) at the live rate.</i>

<u>Usage:</u>
<b>/usdton &lt;amount&gt;</b> — Convert that many USD to TON.

See also <b>/ton</b> to convert TON to USD.`
}

// ---------------------------------------------------------------------
// /calc
// ---------------------------------------------------------------------

func calcHandler(m *tg.NewMessage) error {
	expr := m.Args()
	if expr == "" {
		m.Reply(fmt.Sprintf(
			"<b>🧮 Calculator</b>\n\nUsage: <code>/%s &lt;expression&gt;</code>\nExample: <code>/%s 25*4+10</code>",
			getCommand(m), getCommand(m),
		))
		return tg.ErrEndGroup
	}

	result, err := utils.EvaluateExpression(expr)
	if err != nil {
		m.Reply(fmt.Sprintf(
			"<b>❌ Couldn't calculate that:</b> %s",
			html.EscapeString(err.Error()),
		))
		return tg.ErrEndGroup
	}

	m.Reply(fmt.Sprintf(
		"<b>🧮 Calculator</b>\n\n<code>%s</code> = <b>%s</b>",
		html.EscapeString(expr),
		utils.FormatCalcResult(result),
	))
	return tg.ErrEndGroup
}

// ---------------------------------------------------------------------
// /font, /fonts
// ---------------------------------------------------------------------

func fontHandler(m *tg.NewMessage) error {
	args := strings.TrimSpace(m.Args())
	if args == "" {
		m.Reply(fmt.Sprintf(
			"<b>🔤 Font Converter</b>\n\nUsage:\n<code>/%s &lt;text&gt;</code> — all styles\n<code>/%s &lt;style&gt; &lt;text&gt;</code> — one style\n\nSee <code>/fonts</code> for the list of style names.",
			getCommand(m), getCommand(m),
		))
		return tg.ErrEndGroup
	}

	parts := strings.SplitN(args, " ", 2)
	if len(parts) == 2 {
		if _, ok := isValidFontStyle(parts[0]); ok {
			converted, err := utils.ConvertFont(parts[1], strings.ToLower(parts[0]))
			if err == nil {
				m.Reply(fmt.Sprintf(
					"<b>%s:</b>\n%s",
					html.EscapeString(utils.FontStyleLabel(strings.ToLower(parts[0]))),
					html.EscapeString(converted),
				))
				return tg.ErrEndGroup
			}
		}
	}

	// No valid style given — show every style for the whole input text.
	all := utils.ConvertFontAll(args)
	var sb strings.Builder
	sb.WriteString("<b>🔤 Font Styles</b>\n\n")
	for _, key := range utils.FontStyleNames() {
		label := utils.FontStyleLabel(key)
		if text, ok := all[label]; ok {
			sb.WriteString(fmt.Sprintf("<b>%s:</b> %s\n", html.EscapeString(label), html.EscapeString(text)))
		}
	}

	m.Reply(sb.String())
	return tg.ErrEndGroup
}

func fontsHandler(m *tg.NewMessage) error {
	var sb strings.Builder
	sb.WriteString("<b>🔤 Available Font Styles</b>\n\n")
	for _, key := range utils.FontStyleNames() {
		sb.WriteString(fmt.Sprintf("• <code>%s</code> — %s\n", key, html.EscapeString(utils.FontStyleLabel(key))))
	}
	sb.WriteString("\nUsage: <code>/font &lt;style&gt; &lt;text&gt;</code>")

	m.Reply(sb.String())
	return tg.ErrEndGroup
}

func isValidFontStyle(candidate string) (string, bool) {
	lc := strings.ToLower(candidate)
	for _, key := range utils.FontStyleNames() {
		if key == lc {
			return key, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------
// /ton, /usdton — live Toncoin price via CoinGecko's free public API
// (no API key required).
// ---------------------------------------------------------------------

var tonHTTPClient = resty.New().SetTimeout(10 * time.Second)

const tonPriceURL = "https://api.coingecko.com/api/v3/simple/price?ids=the-open-network&vs_currencies=usd"

type coinGeckoTonResponse struct {
	TheOpenNetwork struct {
		USD float64 `json:"usd"`
	} `json:"the-open-network"`
}

func fetchTonPriceUSD() (float64, error) {
	var result coinGeckoTonResponse

	resp, err := tonHTTPClient.R().
		SetResult(&result).
		Get(tonPriceURL)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch TON price: %w", err)
	}
	if resp.StatusCode() != 200 {
		return 0, fmt.Errorf("price provider returned status %d", resp.StatusCode())
	}
	if result.TheOpenNetwork.USD <= 0 {
		return 0, fmt.Errorf("price provider returned an invalid price")
	}

	return result.TheOpenNetwork.USD, nil
}

func tonHandler(m *tg.NewMessage) error {
	price, err := fetchTonPriceUSD()
	if err != nil {
		m.Reply(fmt.Sprintf("<b>❌ Couldn't fetch the live TON price right now.</b>\n<i>%s</i>", html.EscapeString(err.Error())))
		return tg.ErrEndGroup
	}

	args := strings.TrimSpace(m.Args())
	if args == "" {
		m.Reply(fmt.Sprintf(
			"<b>💎 Toncoin (TON)</b>\n\n1 TON ≈ <b>$%s</b> USD\n\n<i>Use /ton &lt;amount&gt; to convert TON → USD, or /usdton &lt;amount&gt; for USD → TON.</i>",
			strconv.FormatFloat(price, 'f', 4, 64),
		))
		return tg.ErrEndGroup
	}

	amount, err := strconv.ParseFloat(strings.ReplaceAll(args, ",", ""), 64)
	if err != nil {
		m.Reply("<b>❌ Invalid amount.</b> Example: <code>/ton 25</code>")
		return tg.ErrEndGroup
	}

	usdValue := amount * price
	m.Reply(fmt.Sprintf(
		"<b>💎 TON → USD</b>\n\n%s TON ≈ <b>$%s</b> USD\n<i>(rate: 1 TON = $%s)</i>",
		strconv.FormatFloat(amount, 'f', -1, 64),
		strconv.FormatFloat(usdValue, 'f', 4, 64),
		strconv.FormatFloat(price, 'f', 4, 64),
	))
	return tg.ErrEndGroup
}

func usdTonHandler(m *tg.NewMessage) error {
	args := strings.TrimSpace(m.Args())
	if args == "" {
		m.Reply(fmt.Sprintf(
			"<b>💵 USD → TON</b>\n\nUsage: <code>/%s &lt;amount&gt;</code>\nExample: <code>/%s 100</code>",
			getCommand(m), getCommand(m),
		))
		return tg.ErrEndGroup
	}

	amount, err := strconv.ParseFloat(strings.ReplaceAll(args, ",", ""), 64)
	if err != nil {
		m.Reply("<b>❌ Invalid amount.</b> Example: <code>/usdton 100</code>")
		return tg.ErrEndGroup
	}

	price, err := fetchTonPriceUSD()
	if err != nil {
		m.Reply(fmt.Sprintf("<b>❌ Couldn't fetch the live TON price right now.</b>\n<i>%s</i>", html.EscapeString(err.Error())))
		return tg.ErrEndGroup
	}

	tonValue := amount / price
	m.Reply(fmt.Sprintf(
		"<b>💵 USD → TON</b>\n\n$%s USD ≈ <b>%s TON</b>\n<i>(rate: 1 TON = $%s)</i>",
		strconv.FormatFloat(amount, 'f', -1, 64),
		strconv.FormatFloat(tonValue, 'f', 4, 64),
		strconv.FormatFloat(price, 'f', 4, 64),
	))
	return tg.ErrEndGroup
}

