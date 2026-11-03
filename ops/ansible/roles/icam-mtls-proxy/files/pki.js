// pki.js — njs: read SAN fields out of the client certificate nginx has already
// verified ($ssl_client_raw_cert, PEM). The sovereign PKI maps (sovereign_pkis.nginx_maps)
// apply each profile's identifier regex to what these return.
//
//   sanUpn(r)        the first SAN otherName 1.3.6.1.4.1.311.20.2.3 (UPN), or ""
//   sanEmails(r)     every SAN rfc822Name, ';'-joined, or ""
//   otherName(r, oid) the first SAN otherName of that OID, or ""
//
// A minimal DER reader: enough of X.509 to find the subjectAltName extension. The
// chain was validated by nginx before this runs; nothing here decides trust.
// Mirrors services/cert-front/cert_front.py (collab-services) parse_leaf().

var UPN_OID = '1.3.6.1.4.1.311.20.2.3';
var SAN_OID = '2.5.29.17';

function tlv(data, pos) {
    var tag = data[pos];
    pos += 1;
    if ((tag & 0x1f) === 0x1f) { throw new Error('long-form tag unsupported'); }
    var length = data[pos];
    pos += 1;
    if (length & 0x80) {
        var n = length & 0x7f;
        length = 0;
        for (var i = 0; i < n; i++) { length = (length * 256) + data[pos + i]; }
        pos += n;
    }
    return { tag: tag, value: data.slice(pos, pos + length), next: pos + length };
}

function children(value) {
    var out = [];
    var pos = 0;
    while (pos < value.length) {
        var t = tlv(value, pos);
        out.push(t);
        pos = t.next;
    }
    return out;
}

function oid(value) {
    var first = value[0];
    var parts = [String(Math.floor(first / 40)), String(first % 40)];
    var acc = 0;
    for (var i = 1; i < value.length; i++) {
        var b = value[i];
        acc = (acc * 128) + (b & 0x7f);
        if (!(b & 0x80)) { parts.push(String(acc)); acc = 0; }
    }
    return parts.join('.');
}

function str(t) {
    if (t.tag === 0x0c || t.tag === 0x13 || t.tag === 0x16 || t.tag === 0x14 || t.tag === 0x1a) {
        return Buffer.from(t.value).toString('utf8');
    }
    if (t.tag === 0x1e) {   // BMPString, UTF-16BE
        var s = '';
        for (var i = 0; i + 1 < t.value.length; i += 2) { s += String.fromCharCode((t.value[i] << 8) | t.value[i + 1]); }
        return s;
    }
    return '';
}

// Every SAN GeneralName of the leaf: [{kind: 'upn'|'email'|'other', oid, value}]
function sanNames(pem) {
    if (!pem) { return []; }
    var b64 = pem.split('\n').filter(function (l) { return l && l.indexOf('-----') !== 0; }).join('');
    var der = Buffer.from(b64, 'base64');
    var cert = children(der)[0];
    var tbs = children(cert.value)[0];
    var fields = children(tbs.value);
    var i = (fields[0].tag === 0xa0) ? 1 : 0;   // explicit version
    var names = [];
    for (var f = i + 6; f < fields.length; f++) {
        if (fields[f].tag !== 0xa3) { continue; }   // extensions
        var exts = children(children(fields[f].value)[0].value);
        for (var e = 0; e < exts.length; e++) {
            var parts = children(exts[e].value);
            if (oid(parts[0].value) !== SAN_OID) { continue; }
            var octets = parts[parts.length - 1].value;
            var general = children(children(octets)[0].value);
            for (var g = 0; g < general.length; g++) {
                var gn = general[g];
                if (gn.tag === 0xa0) {               // otherName
                    var on = children(gn.value);
                    var o = oid(on[0].value);
                    var inner = children(on[1].value)[0];
                    names.push({ kind: o === UPN_OID ? 'upn' : 'other', oid: o, value: str(inner) });
                } else if (gn.tag === 0x81) {        // rfc822Name
                    // rfc822Name is IA5 (7-bit); utf8 decodes it identically and njs has no 'ascii' codec.
                    names.push({ kind: 'email', oid: '', value: Buffer.from(gn.value).toString('utf8') });
                }
            }
        }
    }
    return names;
}

function safeNames(r) {
    try {
        return sanNames(r.variables.ssl_client_raw_cert);
    } catch (e) {
        // Logged, not raised: a certificate this reader cannot parse yields no identifier
        // and the location refuses it; the reason is in the nginx error log.
        r.error('pki.js: ' + (e && e.message ? e.message : String(e)));
        return [];
    }
}

function sanUpn(r) {
    var n = safeNames(r).filter(function (x) { return x.kind === 'upn'; });
    return n.length ? n[0].value : '';
}

function sanEmails(r) {
    return safeNames(r).filter(function (x) { return x.kind === 'email'; }).map(function (x) { return x.value; }).join(';');
}

function otherName(r, want) {
    var n = safeNames(r).filter(function (x) { return x.kind !== 'email' && x.oid === want; });
    return n.length ? n[0].value : '';
}

export default { sanUpn: sanUpn, sanEmails: sanEmails, otherName: otherName, sanNames: sanNames };
