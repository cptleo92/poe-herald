package pobbin

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultReadLimit = 512 << 10 // 512KB

type BuildSkillsContext struct {
	Content   string
	Truncated bool
}

func FetchBuildSkillsContext(ctx context.Context, userInputURL, userAgent string, maxOutputBytes int) (BuildSkillsContext, error) {
	rawURL, err := normalizeRawURL(userInputURL)
	if err != nil {
		return BuildSkillsContext{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return BuildSkillsContext{}, fmt.Errorf("creating pobb.in request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/xml,text/xml;q=0.9,*/*;q=0.8")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return BuildSkillsContext{}, fmt.Errorf("fetching pobb.in raw XML: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return BuildSkillsContext{}, fmt.Errorf("pobb.in returned status %d", resp.StatusCode)
	}

	xmlBody, err := io.ReadAll(io.LimitReader(resp.Body, defaultReadLimit))
	if err != nil {
		return BuildSkillsContext{}, fmt.Errorf("reading pobb.in response: %w", err)
	}

	content, err := extractBuildSkillsItems(xmlBody)
	if err != nil {
		return BuildSkillsContext{}, err
	}

	if maxOutputBytes > 0 && len(content) > maxOutputBytes {
		return BuildSkillsContext{
			Content:   content[:maxOutputBytes],
			Truncated: true,
		}, nil
	}

	return BuildSkillsContext{Content: content}, nil
}

func normalizeRawURL(input string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(input))
	if err != nil {
		return "", fmt.Errorf("invalid URL")
	}

	if u.Scheme != "https" {
		return "", fmt.Errorf("URL must start with https://")
	}

	host := strings.ToLower(u.Hostname())
	if host != "pobb.in" && host != "www.pobb.in" {
		return "", fmt.Errorf("only https://pobb.in URLs are supported")
	}

	path := strings.TrimSpace(u.EscapedPath())
	if path == "" || path == "/" {
		return "", fmt.Errorf("missing pobb.in build path")
	}

	if !strings.HasSuffix(path, "/xml") {
		path = strings.TrimSuffix(path, "/") + "/xml"
	}

	u.Host = "pobb.in"
	u.RawPath = ""
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// extractBuildSkillsItems returns Build, Skills, and Items top-level sections when present.
// Build and Skills are required; Items is optional (included when the tag exists).
func extractBuildSkillsItems(xmlBody []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(xmlBody))

	var buildXML, skillsXML, itemsXML string

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("invalid Path of Building XML")
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}

		switch start.Name.Local {
		case "Build":
			if buildXML == "" {
				section, err := readSection(decoder, start)
				if err != nil {
					return "", fmt.Errorf("failed to parse Build section")
				}
				buildXML = section
			}
		case "Skills":
			if skillsXML == "" {
				section, err := readSection(decoder, start)
				if err != nil {
					return "", fmt.Errorf("failed to parse Skills section")
				}
				skillsXML = section
			}
		case "Items":
			if itemsXML == "" {
				section, err := readSection(decoder, start)
				if err != nil {
					return "", fmt.Errorf("failed to parse Items section")
				}
				itemsXML = section
			}
		}
	}

	if buildXML == "" || skillsXML == "" {
		return "", fmt.Errorf("pobb.in XML missing Build or Skills section")
	}

	out := buildXML + "\n" + skillsXML
	if itemsXML != "" {
		out += "\n" + itemsXML
	}
	return out, nil
}

func readSection(decoder *xml.Decoder, start xml.StartElement) (string, error) {
	var inner struct {
		Inner string `xml:",innerxml"`
	}
	if err := decoder.DecodeElement(&inner, &start); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(start.Name.Local)
	for _, attr := range start.Attr {
		b.WriteByte(' ')
		b.WriteString(attr.Name.Local)
		b.WriteString(`="`)
		b.WriteString(xmlEscape(attr.Value))
		b.WriteByte('"')
	}
	b.WriteByte('>')
	b.WriteString(inner.Inner)
	b.WriteString("</")
	b.WriteString(start.Name.Local)
	b.WriteByte('>')
	return b.String(), nil
}

func xmlEscape(v string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"<", "&lt;",
		">", "&gt;",
		"'", "&apos;",
	)
	return replacer.Replace(v)
}
