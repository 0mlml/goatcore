// Per-language "subscribe and parse XML" snippets — the XML/XSD
// counterpart of examples.go. Templated from the topic key + the declared
// root element so a consumer can copy-paste a working starting point in
// Rust / Go / Python / TypeScript for an xml:<Root> topic. Reuses the
// CodeExample output struct + the shared template helpers so the viewer's
// code tabs are identical to the protobuf ones.
//
// Like the protobuf snippets these are starting points, not paste-ready
// production code: they assume the consumer models the rest of the .xsd's
// fields on the generated type.

package decoder

import (
	"bytes"
	"text/template"
)

// xsdHelpers extends the shared example helpers with trimAt for stripping
// the renderer's "@" attribute prefix inside the XML templates.
var xsdHelpers = func() template.FuncMap {
	fm := template.FuncMap{"trimAt": trimAt}
	for k, v := range helpers {
		fm[k] = v
	}
	return fm
}()

// mustXSDTmpl parses an XML-example template with the extended helper set.
func mustXSDTmpl(name, body string) *template.Template {
	return template.Must(template.New(name).Funcs(xsdHelpers).Parse(body))
}

// XSDExampleParams carries what the XML templates need.
type XSDExampleParams struct {
	// TopicKey — the full Zenoh key the topic is published on.
	TopicKey string
	// RootElement — the document root element declared by the topic's
	// xml:<Root> format (e.g. "event" for Cursor-on-Target).
	RootElement string
	// SampleField — a representative child element or attribute to show
	// in the snippet body (auto-picked from the rendered .xsd; falls back
	// to a placeholder). Attribute names arrive with the "@" prefix the
	// renderer uses; templates strip it where a bare name is needed.
	SampleField string
}

// RenderXSDCodeExamples returns one snippet per supported language,
// templated against the params. Returns nil when there isn't enough to
// template (empty RootElement / TopicKey), so the viewer hides the
// section — mirroring RenderCodeExamples.
func RenderXSDCodeExamples(p XSDExampleParams) []CodeExample {
	if p.RootElement == "" || p.TopicKey == "" {
		return nil
	}
	if p.SampleField == "" {
		p.SampleField = "field"
	}
	out := make([]CodeExample, 0, len(xsdExampleTemplates))
	for _, t := range xsdExampleTemplates {
		var buf bytes.Buffer
		if err := t.tmpl.Execute(&buf, p); err != nil {
			continue
		}
		out = append(out, CodeExample{
			Language:     t.lang,
			DisplayName:  t.display,
			HighlightTag: t.hljs,
			Code:         buf.String(),
			Source:       "autogen",
		})
	}
	return out
}

// trimAt strips the renderer's "@" attribute prefix for use as a bare
// identifier in snippet field names.
func trimAt(s string) string {
	if len(s) > 0 && s[0] == '@' {
		return s[1:]
	}
	return s
}

var xsdExampleTemplates = []exampleTemplate{
	{
		lang:    "rust",
		display: "Rust",
		hljs:    "rust",
		tmpl: mustXSDTmpl("rust", `// Cargo.toml dependencies:
//   serde     = { version = "1", features = ["derive"] }
//   quick-xml = { version = "0.36", features = ["serialize"] }
//   zenoh     = "1.9"
//   tokio     = { version = "1", features = ["full"] }
//   anyhow    = "1"
//
// Model the rest of the <{{.RootElement}}> element from your .xsd on the
// struct below; quick-xml's serde support deserializes attributes and
// child elements by name (attributes take the "@" rename).

use serde::Deserialize;
use zenoh::Config;

#[derive(Debug, Deserialize)]
struct {{pascalCase .RootElement}} {
    #[serde(rename = "{{.SampleField}}")]
    {{snakeCase (trimAt .SampleField)}}: Option<String>,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let session = zenoh::open(Config::default()).await?;
    let sub = session.declare_subscriber("{{.TopicKey}}").await?;
    while let Ok(sample) = sub.recv_async().await {
        let xml = String::from_utf8(sample.payload().to_bytes().to_vec())?;
        let doc: {{pascalCase .RootElement}} = quick_xml::de::from_str(&xml)?;
        println!("{:?}", doc);
    }
    Ok(())
}
`),
	},
	{
		lang:    "go",
		display: "Go",
		hljs:    "go",
		tmpl: mustXSDTmpl("go", `// go.mod:
//   require github.com/eclipse-zenoh/zenoh-go v1.9.0
//
// encoding/xml maps struct tags to the <{{.RootElement}}> element; add the
// remaining fields from your .xsd (attributes use `+"`xml:\"name,attr\"`"+`).

package main

import (
	"encoding/xml"
	"fmt"

	zenoh "github.com/eclipse-zenoh/zenoh-go"
)

type {{pascalCase .RootElement}} struct {
	XMLName xml.Name `+"`xml:\"{{.RootElement}}\"`"+`
	{{pascalCase (trimAt .SampleField)}} string `+"`xml:\"{{.SampleField}}\"`"+`
}

func main() {
	session, err := zenoh.Open(zenoh.NewConfig())
	if err != nil {
		panic(err)
	}
	defer session.Close()

	sub, _ := session.DeclareSubscriber("{{.TopicKey}}")
	for sample := range sub.Receiver() {
		var doc {{pascalCase .RootElement}}
		if err := xml.Unmarshal(sample.Payload(), &doc); err != nil {
			continue
		}
		fmt.Printf("%+v\n", doc)
	}
}
`),
	},
	{
		lang:    "python",
		display: "Python",
		hljs:    "python",
		tmpl: mustXSDTmpl("python", `# pip install eclipse-zenoh
#   (optional, for full .xsd validation:  pip install xmlschema)

import zenoh
import xml.etree.ElementTree as ET

# import xmlschema
# schema = xmlschema.XMLSchema("your.xsd")   # then schema.validate(payload)


def on_sample(sample: zenoh.Sample):
    root = ET.fromstring(bytes(sample.payload))
    # root.tag == "{{.RootElement}}"
    print("attrs:", root.attrib)
    print("children:", {child.tag: child.text for child in root})


def main():
    with zenoh.open(zenoh.Config()) as session:
        session.declare_subscriber("{{.TopicKey}}", on_sample)
        input("Subscribed to {{.TopicKey}} — press Enter to quit\n")


if __name__ == "__main__":
    main()
`),
	},
	{
		lang:    "typescript",
		display: "TypeScript",
		hljs:    "typescript",
		tmpl: mustXSDTmpl("typescript", `// npm install @eclipse-zenoh/zenoh-ts fast-xml-parser
//
// fast-xml-parser turns the <{{.RootElement}}> document into a plain object;
// attributes are surfaced with the "@" prefix configured below.

import { Session, Config } from "@eclipse-zenoh/zenoh-ts";
import { XMLParser } from "fast-xml-parser";

const parser = new XMLParser({ ignoreAttributes: false, attributeNamePrefix: "@" });

const session = await Session.open(new Config("tcp/localhost:7447"));
await session.declareSubscriber("{{.TopicKey}}", {
  handler: (sample) => {
    const xml = sample.payload().toString();
    const doc = parser.parse(xml);
    console.log(doc["{{.RootElement}}"]);
  },
});
`),
	},
}
